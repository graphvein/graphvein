"""Assign the inventory's disagreement class IDs to oracle rows (T020 S3).

Usage: python3 -I classify.py OUT_DIR [OUT59_DIR] > inventory.jsonl

Reads <corpus>.summary.json and <corpus>.rows.jsonl written by oracle.cjs
(TypeScript 6.0.3) and, when given, the TypeScript 5.9.3 run, and prints
one JSON object per line: per-corpus summary and census rows, every
disagreement row with its class ID (the T020 task record (internal),
section 5), and the sites whose outcome differs between the two compiler
versions. The class rules here are the inventory's definitions.
"""
import glob
import json
import os
import sys


def decl_class(r, renamed_files, misparsed_files):
    cls = r["class"]
    g = r.get("graphvein") or r.get("identity") or ""
    o = r.get("oracle") or ""
    base = cls.replace("syntax_error_file:", "")
    if cls.startswith("syntax_error_file:"):
        return "D-4"
    if "default." in g and "default." not in o:
        return "D-1"
    if base.startswith("oracle_only") and (r["corpus"], r["file"]) in renamed_files:
        return "D-1"
    # An arrow whose body is a tagged template with type arguments
    # (`=> sql<T>`...``): tree-sitter reads a call of the arrow, so the
    # binding is a variable (exported) or nothing (not exported).
    if base.startswith("kind_differs") or base.startswith("oracle_only:const_arrow"):
        return "D-3"
    if base.startswith("exported+default_export") or base.startswith("exported:"):
        return "D-1"
    if base == "graphvein_only" and "%40overload" in g:
        return "D-2"
    if base.startswith("kind_differs") or (base.startswith("oracle_only:const_arrow") and (r["corpus"], r["file"]) in misparsed_files):
        return "D-3"
    if "decorators" in base:
        return "D-6"
    if base.startswith("span:") and "signature" in base:
        return "D-5b"
    if base.startswith("span_start"):
        return "D-5a"
    if base.startswith("span:type_alias"):
        return "D-5c"
    return "D-other"


def import_class(r):
    cls = r["class"]
    if cls == "WRONG_different_file/declaration_vs_implementation":
        return "I-1"
    if "workspace_entry_not_found_by_compiler" in cls:
        return "I-2"
    if "governing_config_differs" in cls:
        return "I-3"
    if "bare_name_mapped_by_config_or_package" in cls or cls.endswith("bare/other"):
        return "I-4"
    if cls.startswith("graphvein_file_oracle_unresolved:relative"):
        return "I-5"
    if cls.startswith("under_resolved"):
        return "I-7"
    if cls.startswith("graphvein_no_site:import_equals"):
        return "I-9"
    return "I-other"


def main():
    out = sys.argv[1]
    out59 = sys.argv[2] if len(sys.argv) > 2 else None
    for path in sorted(glob.glob(os.path.join(out, "*.summary.json"))):
        s = json.load(open(path))
        c = s["corpus"]
        print(json.dumps({"kind": "corpus_summary", "corpus": c, "typescript": s["typescript"], "stats": s["stats"], "workspace_roots": s["workspace_roots"], "governing_vs_main_config": s["governing_vs_main_config"], "resolution_kinds": s["resolution_kinds"]}, sort_keys=True))
        print(json.dumps({"kind": "census", "corpus": c, "forms": s["census"]}, sort_keys=True))
        rows = [json.loads(line) for line in open(os.path.join(out, c + ".rows.jsonl")) if line.strip()]
        renamed = {(r["corpus"], r["file"]) for r in rows if r["kind"] == "decl_disagreement" and "default." in (r.get("graphvein") or "")}
        kinds = {(r["corpus"], r["file"]) for r in rows if r["kind"] == "decl_disagreement" and r["class"].startswith("kind_differs")}
        for r in rows:
            if r["kind"] == "decl_disagreement":
                cid = decl_class(r, renamed, kinds)
            elif r["kind"] == "import_disagreement":
                cid = import_class(r)
            else:
                cid = "F-1" if r["class"] == "graphvein_syntax_error_ts_accepts" else "F-2"
            r["class_id"] = cid
            # Locations only: no third-party source text in the committed data.
            r.pop("text", None)
            print(json.dumps(r, sort_keys=True))
    if out59:
        for path in sorted(glob.glob(os.path.join(out59, "*.rows.jsonl"))):
            c = os.path.basename(path)[: -len(".rows.jsonl")]
            new = {(r.get("file"), r.get("line"), r.get("spec")): r for r in (json.loads(x) for x in open(os.path.join(out, c + ".rows.jsonl")) if x.strip()) if r["kind"] == "import_disagreement"}
            for line in open(path):
                r = json.loads(line)
                if r["kind"] != "import_disagreement":
                    continue
                key = (r.get("file"), r.get("line"), r.get("spec"))
                if key in new and new[key]["class"] == r["class"]:
                    continue
                r["kind"] = "ts59_differential"
                r["class_id"] = "I-10"
                r["class_6_0"] = new[key]["class"] if key in new else "agree"
                r.pop("text", None)
                print(json.dumps(r, sort_keys=True))


if __name__ == "__main__":
    main()
