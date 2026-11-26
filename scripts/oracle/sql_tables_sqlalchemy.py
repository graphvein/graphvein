"""The SQLAlchemy fixture oracle of T018h (rows CT-P1, CT-P2; ADR 0060).

Run it in a scratch virtual environment with the pinned libraries
(SQLModel 0.0.39, SQLAlchemy 2.0.51, Pydantic 2.13.4), never inside the
repository, with -I -B so nothing is written next to the sources:

    python -I -B sql_tables_sqlalchemy.py --root ROOT --requests FILE \
        --ddl FILE --out FILE

--requests is the JSON TestSQLAlchemyOracle (phase requests) wrote: one
entry per claimed CT-P1/CT-P2 site with its module, the statement's source
text and the claimed (reads, writes) table names. For each site the script
imports the module (fastapi and alembic are stubbed: the fixture only
imports Depends and op), evaluates the statement text in the module's
namespace, compiles it with the Postgres dialect and reads its tables:

  - a Select: every Table under its final FROM list (joins and aliases
    unwrapped), read;
  - an Insert, Update or Delete: its target table, written (the select of
    insert().from_select() is a site of its own).

--ddl receives CREATE TABLE statements for SQLModel.metadata compiled with
the Postgres dialect (the schema the SQL oracle applies for CT-P5).
A site with a partial unknown record agrees when its claims are a subset
of the statement's tables. --out receives the per-site comparison. The exit status is 1 when any site
disagrees. The script is never run by CI.
"""

import argparse
import importlib
import json
import sys
import types


def stub_modules():
    fastapi = types.ModuleType("fastapi")
    fastapi.Depends = lambda dependency=None: dependency
    sys.modules.setdefault("fastapi", fastapi)
    alembic = types.ModuleType("alembic")
    alembic.op = types.SimpleNamespace()
    sys.modules.setdefault("alembic", alembic)


def base_tables(from_clause):
    from sqlalchemy import Table
    from sqlalchemy.sql import util as sql_util

    out = set()
    for t in sql_util.find_tables(from_clause, include_joins=True, include_aliases=True):
        while not isinstance(t, Table) and hasattr(t, "element"):
            t = t.element
        if isinstance(t, Table):
            out.add(t.name if t.schema is None else t.schema + "." + t.name)
    return out


def statement_tables(stmt):
    from sqlalchemy.sql.dml import Delete, Insert, Update
    from sqlalchemy.sql.selectable import Select

    if isinstance(stmt, (Insert, Update, Delete)):
        t = stmt.table
        return set(), {t.name if t.schema is None else t.schema + "." + t.name}
    if isinstance(stmt, Select):
        reads = set()
        for f in stmt.get_final_froms():
            reads |= base_tables(f)
        return reads, set()
    raise TypeError("not a statement: %s" % type(stmt).__name__)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True)
    parser.add_argument("--requests", required=True)
    parser.add_argument("--ddl", required=True)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()
    sys.path.insert(0, args.root)
    stub_modules()
    from sqlalchemy.dialects import postgresql
    from sqlalchemy.schema import CreateTable
    from sqlmodel import SQLModel

    with open(args.requests) as f:
        requests = json.load(f)
    results = []
    disagreements = 0
    for r in requests:
        entry = {"key": r["key"], "row": r["row"], "text": r["text"]}
        try:
            module = importlib.import_module(r["module"])
            stmt = eval(r["text"], dict(vars(module)))
            str(stmt.compile(dialect=postgresql.dialect()))
            reads, writes = statement_tables(stmt)
            entry["reads"], entry["writes"] = sorted(reads), sorted(writes)
            if r.get("partial"):
                # A partial site claims a lower bound: every claim must be
                # one of the statement's tables.
                entry["agree"] = set(r["reads"]) <= reads and set(r["writes"]) <= writes
            else:
                entry["agree"] = entry["reads"] == sorted(r["reads"]) and entry["writes"] == sorted(r["writes"])
        except Exception as e:  # a site the oracle cannot evaluate disagrees
            entry["error"] = "%s: %s" % (type(e).__name__, e)
            entry["agree"] = False
        if not entry["agree"]:
            disagreements += 1
            print("DISAGREEMENT %s %s: claimed %s/%s, sqlalchemy %s" % (r["key"], r["text"], r["reads"], r["writes"], entry.get("error") or (entry["reads"], entry["writes"])))
        results.append(entry)
    with open(args.ddl, "w") as f:
        for t in SQLModel.metadata.sorted_tables:
            f.write(str(CreateTable(t).compile(dialect=postgresql.dialect())).strip() + ";\n")
    with open(args.out, "w") as f:
        json.dump({"sites": len(results), "disagreements": disagreements, "results": results}, f, indent=2)
        f.write("\n")
    print("oracle: %d sites compared, %d disagreements" % (len(results), disagreements))
    return 1 if disagreements else 0


if __name__ == "__main__":
    sys.exit(main())
