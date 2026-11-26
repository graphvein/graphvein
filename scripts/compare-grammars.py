#!/usr/bin/env python3
"""Offline module comparison; use --require-identical for the corrected C/D runs."""
import argparse
import hashlib
import json
from pathlib import Path
import re


def verified(directory):
    for line in (directory / "SHA256SUMS").read_text().splitlines():
        digest, name = line.split("  ", 1)
        if Path(name).name != name or not re.fullmatch(r"[0-9a-f]{64}", digest):
            raise ValueError("invalid checksum record")
        if hashlib.sha256((directory / name).read_bytes()).hexdigest() != digest:
            raise ValueError(f"checksum mismatch: {directory / name}")


def compare(first, second):
    verified(first)
    verified(second)
    results = []
    for name in ("html", "angular", "c_sharp", "dart", "javascript", "typescript", "tsx"):
        a = (first / f"wasitter-{name}.wasm").read_bytes()
        b = (second / f"wasitter-{name}.wasm").read_bytes()
        records = [json.loads((directory / f"{name}-build.json").read_text()) for directory in (first, second)]
        keys = ("grammar", "tools", "wasi_sdk_archive_sha256", "wasitter_module_sum", "wasitter_go_mod_sum", "clang_version", "cli_version")
        if any(records[0][key] != records[1][key] for key in keys):
            raise ValueError(f"{name}: build inputs differ")
        pattern = rb"/tmp/tmp[a-z0-9_]{8}/"
        results.append(dict(grammar=name, bytes_a=len(a), bytes_b=len(b),
                            sha256_a=hashlib.sha256(a).hexdigest(), sha256_b=hashlib.sha256(b).hexdigest(),
                            differing_bytes=sum(x != y for x, y in zip(a, b)) + abs(len(a)-len(b)),
                            temp_path_occurrences_a=len(re.findall(pattern, a)),
                            temp_path_occurrences_b=len(re.findall(pattern, b)),
                            identical_after_temp_prefix_normalization=re.sub(pattern, b"/constant/", a) == re.sub(pattern, b"/constant/", b)))
    return dict(run_a=records[0]["run_id"], run_b=records[1]["run_id"],
                commit_a=records[0]["graphvein_commit"], commit_b=records[1]["graphvein_commit"], modules=results)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("first", type=Path)
    parser.add_argument("second", type=Path)
    parser.add_argument("--require-identical", action="store_true")
    args = parser.parse_args()
    result = compare(args.first, args.second)
    print(json.dumps(result, indent=2))
    if args.require_identical and any(row["differing_bytes"] for row in result["modules"]):
        raise SystemExit("modules are not byte-identical")
