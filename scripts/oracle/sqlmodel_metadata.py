"""Dump SQLModel table metadata as JSON: the oracle of T018c2 (ADR 0054).

Run it in a scratch virtual environment that has the pinned libraries
(SQLModel 0.0.39, SQLAlchemy 2.0.51, Pydantic 2.13.4; email-validator 2.3.0
for EmailStr annotations), never inside the repository, with -I -B so
nothing is written next to the sources:

    python -I -B sqlmodel_metadata.py --root REPO --path DIR [--path DIR...] \
        [--isolate] MODULE [MODULE...] > oracle.json

--path adds an import root (relative to --root). --isolate imports each
module in its own interpreter (fixture modules share table names), and a
module whose import fails is recorded with its error class. The output maps
"<file>#<class>" (file relative to --root) to the table SQLAlchemy built.
Graphvein's TestModelOracle (internal/extract/models) compares its claims
with this output. The script is never run by CI.
"""

import argparse
import importlib
import json
import os
import subprocess
import sys


def column_facts(col):
    t = col.type
    default = "absent"
    value = None
    if col.default is not None:
        if col.default.is_callable:
            default = "callable"
        elif getattr(col.default, "is_scalar", False):
            default = "scalar"
            value = repr(col.default.arg)
    fks = []
    for fk in sorted(col.foreign_keys, key=lambda f: f.target_fullname):
        target = fk.target_fullname
        if "." not in target:
            fks.append({"schema": None, "table": None, "column": target, "on_delete": fk.ondelete})
            continue
        table, column = target.rsplit(".", 1)
        schema = None
        if "." in table:
            schema, table = table.rsplit(".", 1)
        fks.append({"schema": schema, "table": table, "column": column, "on_delete": fk.ondelete})
    return {
        "type": type(t).__name__,
        "length": getattr(t, "length", None),
        "timezone": getattr(t, "timezone", None),
        "precision": getattr(t, "precision", None),
        "scale": getattr(t, "scale", None),
        "nullable": bool(col.nullable),
        "primary_key": bool(col.primary_key),
        "index": bool(col.index),
        "unique": bool(col.unique),
        "default": default,
        "default_value": value,
        "server_default": col.server_default is not None,
        "foreign_keys": fks,
    }


def module_file(module):
    # A module whose import failed is no longer in sys.modules, so its file
    # is found on the import path instead of through inspect.
    parts = module.split(".")
    for base in sys.path:
        for candidate in (os.path.join(base, *parts) + ".py", os.path.join(base, *parts, "__init__.py")):
            if os.path.isfile(candidate):
                return os.path.abspath(candidate)
    return None


def dump(root):
    from sqlalchemy import CheckConstraint, UniqueConstraint
    from sqlmodel import SQLModel

    out = {}
    for manager in list(SQLModel._sa_registry._managers):
        try:
            mapper = manager.mapper
            cls = mapper.class_
            table = mapper.local_table
            if table is None or table.key not in SQLModel.metadata.tables or SQLModel.metadata.tables[table.key] is not table:
                continue
        except Exception:  # a class whose creation failed half way is not in the metadata
            continue
        source = module_file(cls.__module__)
        if source is None:
            continue
        source = os.path.relpath(source, root).replace(os.sep, "/")
        key = source + "#" + cls.__qualname__
        out[key] = {
            "name": table.name,
            "schema": table.schema,
            "columns": {c.name: column_facts(c) for c in table.columns},
            "primary_key": sorted(c.name for c in table.primary_key.columns),
            "indexes": sorted(
                ({"name": None if ix.name is None else str(ix.name), "unique": bool(ix.unique), "columns": [c.name for c in ix.columns]} for ix in table.indexes),
                key=lambda x: str(x["name"]),
            ),
            "unique_constraints": sorted(
                ({"name": None if c.name is None else str(c.name), "columns": [x.name for x in c.columns]} for c in table.constraints if isinstance(c, UniqueConstraint)),
                key=lambda x: (str(x["name"]), x["columns"]),
            ),
            "check_constraints": sorted(
                ({"name": None if c.name is None else str(c.name), "sqltext": str(c.sqltext)} for c in table.constraints if isinstance(c, CheckConstraint)),
                key=lambda x: (str(x["name"]), x["sqltext"]),
            ),
        }
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", required=True)
    ap.add_argument("--path", action="append", default=[])
    ap.add_argument("--isolate", action="store_true")
    ap.add_argument("modules", nargs="+")
    args = ap.parse_args()
    root = os.path.abspath(args.root)
    if args.isolate:
        tables, errors = {}, {}
        for m in args.modules:
            cmd = [sys.executable, "-I", "-B", os.path.abspath(__file__), "--root", root]
            for p in args.path:
                cmd += ["--path", p]
            res = subprocess.run(cmd + [m], capture_output=True, text=True, check=True)
            part = json.loads(res.stdout)
            tables.update(part["tables"])
            errors.update(part["errors"])
        json.dump({"tables": tables, "errors": errors}, sys.stdout, indent=1, sort_keys=True)
        return
    for p in args.path:
        sys.path.insert(0, os.path.join(root, p))
    errors = {}
    for m in args.modules:
        try:
            importlib.import_module(m)
        except Exception as e:  # the oracle records failures, it never stops
            errors[m] = type(e).__name__
    tables = dump(root)
    try:
        from sqlalchemy.orm import configure_mappers

        configure_mappers()
    except Exception as e:  # relationship errors do not change table metadata (PF7)
        errors["configure_mappers"] = type(e).__name__
    json.dump({"tables": tables, "errors": errors}, sys.stdout, indent=1, sort_keys=True)


if __name__ == "__main__":
    main()
