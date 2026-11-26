#!/usr/bin/env python3
"""Offline orchestration checks: no build, source download or tool installation."""
import hashlib
import io
import json
from pathlib import Path
import runpy
import tarfile
import tempfile
import unittest
from unittest.mock import patch

builder = runpy.run_path(str(Path(__file__).with_name("build-grammars.py")))
comparison = runpy.run_path(str(Path(__file__).with_name("compare-grammars.py")))


def archive(name, link=False):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode="w:gz") as stream:
        item = tarfile.TarInfo(name)
        if link:
            item.type = tarfile.SYMTYPE
            item.linkname = "../../escape"
            stream.addfile(item)
        else:
            item.size = 4
            stream.addfile(item, io.BytesIO(b"test"))
    return output.getvalue()


def comparison_bundle(directory, prefix, changed_source=False):
    directory.mkdir()
    for name in ("html", "angular", "c_sharp", "dart", "javascript", "typescript", "tsx"):
        count = 10 if name == "dart" else 11
        (directory / f"wasitter-{name}.wasm").write_bytes(b"\x00asm" + (prefix + b"lib.c\x00") * count)
        record = dict(grammar={"source_sha256": "changed" if changed_source else "same"},
                      tools={"compiler": "pinned"}, wasi_sdk_archive_sha256="same",
                      wasitter_module_sum="same", wasitter_go_mod_sum="same",
                      clang_version="same", cli_version="same", run_id=directory.name,
                      graphvein_commit="same")
        (directory / f"{name}-build.json").write_text(json.dumps(record))
    (directory / "SHA256SUMS").write_text("".join(
        f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n"
        for path in sorted(directory.iterdir())))


class ComparisonChecks(unittest.TestCase):
    def test_identical_modules(self):
        with tempfile.TemporaryDirectory() as work:
            first, second = Path(work) / "a", Path(work) / "b"
            for directory in (first, second):
                comparison_bundle(directory, b"/opt/graphvein-build/work/")
            result = comparison["compare"](first, second)
            self.assertTrue(all(row["differing_bytes"] == 0 for row in result["modules"]))

    def test_only_random_temp_prefixes_normalize(self):
        with tempfile.TemporaryDirectory() as work:
            first, second = Path(work) / "a", Path(work) / "b"
            comparison_bundle(first, b"/tmp/tmpabcdefgh/")
            comparison_bundle(second, b"/tmp/tmpijklmnop/")
            result = comparison["compare"](first, second)
            self.assertEqual([row["differing_bytes"] for row in result["modules"]], [88, 88, 88, 80, 88, 88, 88])
            self.assertTrue(all(row["identical_after_temp_prefix_normalization"] for row in result["modules"]))
            third = Path(work) / "c"
            comparison_bundle(third, b"/opt/different-source/")
            result = comparison["compare"](first, third)
            self.assertFalse(any(row["identical_after_temp_prefix_normalization"] for row in result["modules"]))

    def test_changed_inputs_rejected(self):
        with tempfile.TemporaryDirectory() as work:
            first, second = Path(work) / "a", Path(work) / "b"
            comparison_bundle(first, b"/fixed/")
            comparison_bundle(second, b"/fixed/", changed_source=True)
            with self.assertRaisesRegex(ValueError, "inputs differ"):
                comparison["compare"](first, second)

    def test_corrupt_asset_rejected_before_comparison(self):
        with tempfile.TemporaryDirectory() as work:
            first, second = Path(work) / "a", Path(work) / "b"
            for directory in (first, second):
                comparison_bundle(directory, b"/fixed/")
            (second / "wasitter-html.wasm").write_bytes(b"corrupt")
            with self.assertRaisesRegex(ValueError, "checksum mismatch"):
                comparison["compare"](first, second)


class BuildChecks(unittest.TestCase):
    def test_module_paths_offline(self):
        builder["check_paths"](b"\x00asm/opt/graphvein-build/work/wasitter/lib.c\x00")
        for path in (b"/tmp/tmp12345678/file.c", b"/home/runner/work/file.c", b"/Users/ci/file.c",
                     b"/private/tmp/file.c", b"/github/workspace/file.c", b"C:\\runner\\file.c"):
            with self.assertRaisesRegex(ValueError, "path"):
                builder["check_paths"](b"\x00asm" + path)
        with patch.dict("os.environ", {"RUNNER_TEMP": "/custom/runner-temp"}):
            with self.assertRaisesRegex(ValueError, "path"):
                builder["check_paths"](b"/custom/runner-temp/file.c")

    def test_fixed_work_refuses_reuse_and_cleans_up(self):
        with tempfile.TemporaryDirectory() as directory:
            fixed = Path(directory) / "work"
            with patch.dict(builder["fixed_work"].__wrapped__.__globals__, {"BUILD_ROOT": fixed}):
                with builder["fixed_work"]() as work:
                    self.assertEqual(work, fixed)
                    with self.assertRaises(FileExistsError):
                        with builder["fixed_work"]():
                            pass
                    (work / "source.c").write_text("test")
                self.assertFalse(fixed.exists())

    def test_all_source_pins_present(self):
        data, rows = builder["manifest"]()
        self.assertEqual(len(data["grammars"]), 11)
        self.assertEqual(len(rows), 7)

    def test_patched_rows_checked(self):
        data, rows = builder["manifest"]()
        patched = {row["name"]: row for row in rows if row.get("patches")}
        self.assertEqual(sorted(patched), ["javascript", "tsx", "typescript"])
        row = dict(patched["javascript"])
        row["patches"] = [dict(row["patches"][0], sha256="0" * 64)]
        with self.assertRaisesRegex(ValueError, "patch checksum"):
            builder["check_patches"](row)
        row = dict(patched["tsx"], patches=[{"path": "../escape.diff", "sha256": "0" * 64}])
        with self.assertRaisesRegex(ValueError, "patch path"):
            builder["check_patches"](row)
        row = dict(patched["typescript"], regenerate="../grammar.json")
        with self.assertRaisesRegex(ValueError, "regeneration path"):
            builder["check_patches"](row)
        row = dict(patched["typescript"], parser_sha256="")
        with self.assertRaisesRegex(ValueError, "parser checksum"):
            builder["check_patches"](row)

    def test_checksum_checked_before_extraction(self):
        blob = archive("source/parser.c")
        with tempfile.TemporaryDirectory() as work:
            with self.assertRaisesRegex(ValueError, "checksum"):
                builder["extract_verified"](blob, "0" * 64, Path(work))
            self.assertEqual(list(Path(work).iterdir()), [])

    def test_safe_source_extracts(self):
        blob = archive("source/parser.c")
        with tempfile.TemporaryDirectory() as work:
            root = builder["extract_verified"](blob, hashlib.sha256(blob).hexdigest(), Path(work))
            self.assertEqual((root / "parser.c").read_bytes(), b"test")

    def test_traversal_and_links_rejected(self):
        for name, link in [("../escape", False), ("/escape", False), ("source/link", True)]:
            blob = archive(name, link)
            with tempfile.TemporaryDirectory() as work:
                with self.assertRaisesRegex(ValueError, "unsafe"):
                    builder["extract_verified"](blob, hashlib.sha256(blob).hexdigest(), Path(work))


if __name__ == "__main__":
    unittest.main()
