#!/usr/bin/env python3
"""CI-only orchestration; uses wasitter's pinned compiler without its registry."""
import hashlib
from contextlib import contextmanager
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys
import tarfile
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
BUILD_ROOT = Path("/opt/graphvein-build/work")
# The grammars this workflow compiles; ADR 0074 adds the three patched,
# regenerated JavaScript/TypeScript/TSX rows to ADR 0007's four.
CI_GRAMMARS = ("html", "angular", "c_sharp", "dart", "javascript", "typescript", "tsx")


@contextmanager
def fixed_work():
    # Refuse an existing directory; never remove work belonging to another run.
    BUILD_ROOT.mkdir()
    try:
        yield BUILD_ROOT
    finally:
        shutil.rmtree(BUILD_ROOT)


def check_paths(blob):
    """Reject host-specific source paths in an offline inspection of WASM bytes."""
    for path in (b"/tmp/", b"/private/", b"/home/", b"/Users/", b"/runner/",
                 b"/workspace/", b"/workspaces/", b"/github/workspace/", b"C:\\", b"D:\\"):
        if path in blob:
            raise ValueError(f"host-specific path in module: {path!r}")
    for name in ("RUNNER_TEMP", "GITHUB_WORKSPACE"):
        path = os.environ.get(name)
        if path and path.encode() in blob:
            raise ValueError(f"{name} path in module")


def manifest():
    # JSON is a YAML subset, avoiding a YAML dependency on developer machines.
    data = json.loads((ROOT / "grammars/manifest.yaml").read_text())
    if not re.fullmatch(r"[0-9a-f]{64}", data["tools"].get("wasi_sdk_sha256", "")):
        raise ValueError("missing reviewed SDK checksum")
    rows = [g for g in data["grammars"] if g["status"] == "built in CI"]
    if {g["name"] for g in rows} != set(CI_GRAMMARS):
        raise ValueError("expected exactly the seven CI grammars")
    for row in rows:
        if not re.fullmatch(r"[0-9a-f]{64}", row["source_sha256"] or ""):
            raise ValueError(f"missing reviewed source checksum: {row['name']}")
        if row["abi"] not in (13, 14, 15):
            raise ValueError(f"unsupported ABI: {row['name']}")
        check_patches(row)
    return data, rows


def check_patches(row):
    """Verify a row's carried grammar patches (ADR 0074) against their reviewed digests.

    A patched row names the generated grammar it regenerates and the parser
    digest that regeneration must reproduce, so a patch that applies but
    yields different tables is refused rather than shipped.
    """
    patches = row.get("patches", [])
    if not patches:
        if "regenerate" in row or "parser_sha256" in row:
            raise ValueError(f"{row['name']}: regeneration without patches")
        return []
    if not re.fullmatch(r"[0-9a-f]{64}", row.get("parser_sha256", "")):
        raise ValueError(f"{row['name']}: missing reviewed parser checksum")
    grammar = PurePosixPath(row.get("regenerate", ""))
    if grammar.name != "grammar.json" or grammar.is_absolute() or ".." in grammar.parts:
        raise ValueError(f"{row['name']}: unsafe regeneration path")
    blobs = []
    for entry in patches:
        path = PurePosixPath(entry["path"])
        if path.parent != PurePosixPath("grammars/patches") or path.suffix != ".diff":
            raise ValueError(f"{row['name']}: unexpected patch path {entry['path']}")
        blob = (ROOT / path).read_bytes()
        if hashlib.sha256(blob).hexdigest() != entry["sha256"]:
            raise ValueError(f"{row['name']}: patch checksum mismatch: {entry['path']}")
        blobs.append(path)
    return blobs


def apply_patches(row, source):
    """Apply reviewed patches and regenerate the parser with the pinned CLI.

    Regeneration reads only the patched src/grammar.json, so it needs no
    Node runtime and no checkout of grammars it was composed from.
    """
    patches = check_patches(row)
    if not patches:
        return
    for path in patches:
        subprocess.run(["git", "apply", "-p1", str(ROOT / path)], cwd=source, check=True)
    grammar = source / row["regenerate"]
    directory = grammar.parent.parent
    subprocess.run(["tree-sitter", "generate", str(grammar.relative_to(directory))], cwd=directory, check=True)
    parser = source / row["parser"]
    if hashlib.sha256(parser.read_bytes()).hexdigest() != row["parser_sha256"]:
        raise ValueError(f"{row['name']}: regenerated parser differs from reviewed checksum")


def extract_verified(blob, digest, destination):
    if hashlib.sha256(blob).hexdigest() != digest:
        raise ValueError("source archive checksum mismatch")
    with tarfile.open(fileobj=io.BytesIO(blob), mode="r:gz") as archive:
        # Reject links and non-file entries before extracting verified sources.
        for member in archive.getmembers():
            path = PurePosixPath(member.name)
            if "\\" in member.name or path.is_absolute() or ".." in path.parts or not (member.isfile() or member.isdir()):
                raise ValueError(f"unsafe archive member: {member.name}")
        archive.extractall(destination, filter="data")
    directories = list(destination.iterdir())
    if len(directories) != 1 or not directories[0].is_dir():
        raise ValueError("expected one source archive root")
    return directories[0]


def command(*args, cwd=None):
    return subprocess.check_output(args, cwd=cwd, text=True).strip()


def build():
    if sys.argv[1:2] == ["--check-paths"] and len(sys.argv) > 2:
        for filename in sys.argv[2:]:
            check_paths(Path(filename).read_bytes())
        return
    data, rows = manifest()
    if sys.argv[1:] == ["--validate"]:
        return
    if sys.argv[1:]:
        raise ValueError("usage: build-grammars.py [--validate]")
    if os.environ.get("GITHUB_ACTIONS") != "true" or sys.platform != "linux":
        raise ValueError("grammar compilation is restricted to Linux CI")
    sdk = Path(os.environ["WASI_SDK_PATH"])
    if os.environ["WASI_SDK_SHA256"] != data["tools"]["wasi_sdk_sha256"]:
        raise ValueError("SDK archive checksum differs from manifest")
    cli_version = command("tree-sitter", "--version")
    if not cli_version.startswith("tree-sitter " + data["tools"]["tree_sitter_cli"] + " "):
        raise ValueError("unexpected tree-sitter CLI version")
    clang_version = command(str(sdk / "bin/clang"), "--version")
    out = ROOT / "grammars/artifacts"
    out.mkdir(parents=True, exist_ok=False)
    with fixed_work() as work:
        # Download inside a disposable module, never changing production go.mod.
        command("go", "mod", "init", "graphvein-grammar-builder", cwd=work)
        info = json.loads(command("go", "mod", "download", "-json",
                                  "github.com/zema1/wasitter@" + data["tools"]["wasitter"], cwd=work))
        adapter = work / "wasitter"
        shutil.copytree(info["Dir"], adapter)
        for path in adapter.rglob("*"):
            path.chmod(0o755 if path.is_dir() else 0o644)
        adapter.chmod(0o755)
        helper = adapter / "cmd/graphvein-build"
        helper.mkdir()
        shutil.copyfile(ROOT / "grammars/builder.go.txt", helper / "main.go")
        executable = work / "grammar-builder"
        command("go", "build", "-trimpath", "-o", str(executable), "./cmd/graphvein-build", cwd=adapter)
        for row in rows:
            destination = work / row["name"]
            destination.mkdir()
            with urllib.request.urlopen(row["source_url"], timeout=120) as response:
                source = extract_verified(response.read(), row["source_sha256"], destination)
            apply_patches(row, source)
            parser = (source / row["parser"]).read_text()
            abi = int(re.search(r"#define LANGUAGE_VERSION (\d+)", parser)[1])
            if abi != row["abi"]:
                raise ValueError(f"{row['name']}: ABI differs from manifest")
            config = work / "row.json"
            config.write_text(json.dumps(row))
            asset = out / ("wasitter-" + row["name"] + ".wasm")
            subprocess.run([str(executable), str(adapter), str(source), str(config), str(asset)], check=True)
            check_paths(asset.read_bytes())
            license_text = (source / "LICENSE").read_text()
            if license_text != (ROOT / "grammars" / (row["name"] + "-LICENSE.txt")).read_text():
                raise ValueError(f"{row['name']}: licence differs from reviewed text")
            # Preserve all nested source licences and notices as well.
            licenses = []
            for path in sorted(source.rglob("*")):
                if path.is_file() and any(word in path.name.lower() for word in ("license", "copyright", "notice")):
                    licenses.append(f"===== {path.relative_to(source)} =====\n{path.read_text()}\n")
            (out / (row["name"] + "-LICENSES.txt")).write_text("\n".join(licenses))
            record = dict(grammar=row, tools=data["tools"], clang_version=clang_version,
                          cli_version=cli_version, wasi_sdk_archive_sha256=os.environ["WASI_SDK_SHA256"], wasitter_module_sum=info["Sum"],
                          wasitter_go_mod_sum=info["GoModSum"],
                          asset_sha256=hashlib.sha256(asset.read_bytes()).hexdigest(),
                          graphvein_commit=os.environ["GITHUB_SHA"], run_id=os.environ["GITHUB_RUN_ID"],
                          loading_evidence="pending T005b")
            (out / (row["name"] + "-build.json")).write_text(json.dumps(record, indent=2) + "\n")
    shutil.copyfile(ROOT / "grammars/manifest.yaml", out / "manifest.yaml")
    shutil.copyfile(ROOT / "grammars/NOTICES.md", out / "NOTICES.md")
    shutil.copyfile(ROOT / "grammars/UPSTREAM_NOTICES.txt", out / "UPSTREAM_NOTICES.txt")
    shutil.copyfile(ROOT / "grammars/LLVM-LICENSE.txt", out / "LLVM-LICENSE.txt")
    shutil.copyfile(ROOT / "grammars/WASI-SDK-NOTICES.txt", out / "WASI-SDK-NOTICES.txt")
    sums = "".join(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n" for p in sorted(out.iterdir()))
    (out / "SHA256SUMS").write_text(sums)


if __name__ == "__main__":
    build()
