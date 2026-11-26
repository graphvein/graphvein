"""Helper of prisma_catalog.sh: the Prisma oracle of T018g1 (ADR 0057).

Subcommands, run with python -I -B so nothing is written next to the
sources:

    prepare SRC DEST DROPS MAJOR
                             copy a schema file or folder to DEST, removing
                             the blocks and fields DROPS names
                             ("Model;Model.field;Model:line"), and print the
                             datasource provider; before Prisma 7 a
                             datasource without url gets
                             url = env("DATABASE_URL"), which 5 and 6
                             require
    sqlite SQL DB OUT        apply Prisma's SQL to a new SQLite file and
                             write its catalog as JSON
    run OUT DIR VERSION PROVIDER SCHEMA DROPS SQL CATALOG
                             write one run record (link directory, version,
                             provider, schema, dropped forms, SQL, catalog)
                             as JSON
    merge OUT RUN...         merge run records into the dump TestPrismaOracle
                             reads

Never run by CI.
"""

import json
import os
import re
import shutil
import sqlite3
import sys

BLOCK = re.compile(r"^(model|view|enum|type)\s+(\w+)\s*\{")


def schema_files(path):
    if os.path.isdir(path):
        out = []
        for root, _dirs, files in os.walk(path):
            for f in files:
                if f.endswith(".prisma"):
                    out.append(os.path.join(root, f))
        return sorted(out)
    return [path]


def drop_forms(text, blocks, fields, lines):
    """Remove whole blocks named in blocks, field lines named in fields
    ({block: {field}}) and lines of a block written exactly as in lines
    ({block: {stripped line}}, for block attributes). A removed block ends
    at its closing brace. Returns the text and the forms removed."""
    out, removed = [], []
    current, skipping = None, False
    for line in text.splitlines(keepends=True):
        m = BLOCK.match(line)
        if m and current is None:
            current = m.group(2)
            skipping = current in blocks
            if skipping:
                removed.append(current)
        elif current is not None and line.strip() == "}":
            was = skipping
            current, skipping = None, False
            if was:
                continue
        elif current is not None and not skipping:
            words = line.split()
            if line.strip() in lines.get(current, set()):
                removed.append(current + ":" + line.strip())
                continue
            if words and words[0] in fields.get(current, set()):
                removed.append(current + "." + words[0])
                continue
        if not skipping:
            out.append(line)
    return "".join(out), removed


DATASOURCE = re.compile(r"^(datasource\s+\w+\s*\{[^}]*?)(\n\})", re.M | re.S)


def prepare(src, dest, drops, major):
    blocks, fields, lines = set(), {}, {}
    for d in filter(None, drops.split(";")):
        if ":" in d:
            b, l = d.split(":", 1)
            lines.setdefault(b, set()).add(l)
        elif "." in d:
            b, f = d.split(".", 1)
            fields.setdefault(b, set()).add(f)
        else:
            blocks.add(d)
    files = schema_files(src)
    removed, provider = [], None
    if os.path.isdir(src):
        os.makedirs(dest)
    for f in files:
        with open(f, encoding="utf-8") as fh:
            text = fh.read()
        text, r = drop_forms(text, blocks, fields, lines)
        removed += r
        m = re.search(r'provider\s*=\s*"(postgresql|sqlite|mysql|sqlserver|cockroachdb)"', text)
        if m and "datasource" in text:
            provider = m.group(1)
            if int(major) < 7 and not re.search(r"^\s*url\s*=", text, re.M):
                text = DATASOURCE.sub(lambda d: d.group(1) + '\n  url = env("DATABASE_URL")' + d.group(2), text, count=1)
        target = os.path.join(dest, os.path.relpath(f, src)) if os.path.isdir(src) else dest
        os.makedirs(os.path.dirname(target) or ".", exist_ok=True)
        with open(target, "w", encoding="utf-8") as fh:
            fh.write(text)
    wanted = sorted(blocks) + sorted(b + "." + f for b, fs in fields.items() for f in fs)
    wanted += sorted(b + ":" + l for b, ls in lines.items() for l in ls)
    missing = sorted(set(wanted) - set(removed))
    if missing:
        sys.exit("prepare: drop names not found in %s: %s" % (src, ", ".join(missing)))
    if provider is None:
        sys.exit("prepare: no literal datasource provider in %s" % src)
    print(provider)


def sqlite_catalog(sql_path, db_path, out_path):
    if os.path.exists(db_path):
        os.remove(db_path)
    with open(sql_path, encoding="utf-8") as fh:
        sql = fh.read()
    conn = sqlite3.connect(db_path)
    conn.executescript(sql)
    tables = []
    for name, ddl in conn.execute(
        "SELECT name, sql FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite\\_%' ESCAPE '\\' ORDER BY name"
    ).fetchall():
        q = '"' + name.replace('"', '""') + '"'
        cols, pk = [], []
        for _cid, cname, ctype, notnull, dflt, pkpos, _hidden in conn.execute("PRAGMA table_xinfo(%s)" % q):
            cols.append({"name": cname, "type": ctype, "not_null": bool(notnull), "default": dflt})
            if pkpos:
                pk.append((pkpos, cname))
        indexes = []
        for _seq, iname, unique, origin, partial in conn.execute("PRAGMA index_list(%s)" % q):
            iq = '"' + iname.replace('"', '""') + '"'
            icols = [r[2] if r[2] is not None else "" for r in conn.execute("PRAGMA index_xinfo(%s)" % iq) if r[5]]
            indexes.append({"name": iname, "unique": bool(unique), "origin": origin, "partial": bool(partial), "columns": icols})
        fks = {}
        for fid, _seq, ref, frm, to, on_update, on_delete, _match in conn.execute("PRAGMA foreign_key_list(%s)" % q):
            fk = fks.setdefault(fid, {"ref_table": ref, "columns": [], "ref_columns": [], "on_update": on_update, "on_delete": on_delete})
            fk["columns"].append(frm)
            fk["ref_columns"].append(to)
        tables.append({
            "schema": None, "name": name, "sql": ddl, "columns": cols,
            "primary_key": [c for _p, c in sorted(pk)],
            "indexes": sorted(indexes, key=lambda i: i["name"]),
            "foreign_keys": [fks[k] for k in sorted(fks)],
        })
    conn.close()
    with open(out_path, "w", encoding="utf-8") as fh:
        json.dump({"tables": tables, "enums": []}, fh, indent=1, sort_keys=True)


def run_record(out, linkdir, version, provider, schema, drops, sql_path, catalog_path):
    with open(sql_path, encoding="utf-8") as fh:
        sql = fh.read()
    with open(catalog_path, encoding="utf-8") as fh:
        catalog = json.load(fh)
    rec = {
        "dir": linkdir, "prisma": version, "provider": provider, "schema": schema,
        "dropped": [d for d in drops.split(";") if d], "sql": sql, "catalog": catalog,
    }
    with open(out, "w", encoding="utf-8") as fh:
        json.dump(rec, fh, indent=1, sort_keys=True)


def merge(out, paths):
    runs = []
    for p in paths:
        with open(p, encoding="utf-8") as fh:
            runs.append(json.load(fh))
    with open(out, "w", encoding="utf-8") as fh:
        json.dump({"runs": runs}, fh, indent=1, sort_keys=True)
        fh.write("\n")


def main(argv):
    cmd = argv[1] if len(argv) > 1 else ""
    if cmd == "prepare" and len(argv) == 6:
        prepare(argv[2], argv[3], argv[4], argv[5])
    elif cmd == "sqlite" and len(argv) == 5:
        sqlite_catalog(argv[2], argv[3], argv[4])
    elif cmd == "run" and len(argv) == 10:
        run_record(*argv[2:])
    elif cmd == "merge" and len(argv) >= 3:
        merge(argv[2], argv[3:])
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main(sys.argv)
