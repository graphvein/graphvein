#!/usr/bin/env python3
"""Measure the precision of route <-> caller links in a Graphvein SQLite store.

Three steps, each reproducible from its inputs:

  export   read every stored calls_route edge of a repository and write one
           JSON line per link with the facts an adjudicator needs (call site,
           URL expression, the path alternative that links, the route and its
           full path templates, tier and confidence). Verdict fields are empty.
  unlinked write one JSON line per production client call without a link
           (status dynamic, unmatched or ambiguous; not generated), for the
           recall sample;
           "area" (the first path segment) is the recall stratum.
  summary  print the stored client_call status counts (unknowns as counts).
  sample   draw a seeded random sample from a JSONL file, optionally
           stratified by a field, keeping at least N per stratum (or all).
  stats    read adjudicated JSONL files and print precision, false-link rate
           and unverifiable share per corpus and tier, with Wilson 95%
           intervals, and the recall estimate from adjudicated unlinked calls.

The store is opened read-only; nothing in the indexed repository is read or
executed. Usage:

  python3 link_precision.py export   STORE.db REPOSITORY > links.jsonl
  python3 link_precision.py unlinked STORE.db REPOSITORY > unlinked.jsonl
  python3 link_precision.py summary  STORE.db REPOSITORY
  python3 link_precision.py sample   FILE.jsonl --seed 32 --per-stratum 150 [--by tier]
  python3 link_precision.py stats    FILE.jsonl [FILE.jsonl ...]
"""
import argparse
import json
import math
import random
import sqlite3
import sys

VERDICTS = ("correct", "wrong", "unverifiable")
UNLINKED_VERDICTS = ("correctly_unlinked", "missed")


def wilson(successes, total, z=1.959964):
    """Wilson score interval for a binomial proportion; (0, 0) when total is 0."""
    if total == 0:
        return (0.0, 0.0)
    p = successes / total
    denom = 1 + z * z / total
    centre = (p + z * z / (2 * total)) / denom
    half = z * math.sqrt(p * (1 - p) / total + z * z / (4 * total * total)) / denom
    return (max(0.0, centre - half), min(1.0, centre + half))


def tier_of(confidence, match, origin):
    """Name the matching tier of docs/extract/clients.md from an edge."""
    if match == "suffix":
        return "suffix-0.6"
    if confidence >= 0.9:
        return "exact-0.9"
    return "exact-origin-0.7"


def line_of(record):
    span = record.get("span") or {}
    return "%s:%s" % (record.get("file"), span.get("start_line"))


def open_store(path):
    return sqlite3.connect("file:%s?mode=ro" % path, uri=True)


def node(db, repository, identity):
    row = db.execute(
        "select payload from nodes where repository=? and identity=?",
        (repository, identity),
    ).fetchone()
    return json.loads(row[0]) if row else None


def export_links(db, repository):
    rows = db.execute(
        "select source, target, payload from edges where repository=? and kind='calls_route' order by source, target",
        (repository,),
    ).fetchall()
    for source, target, payload in rows:
        edge = json.loads(payload)
        call = node(db, repository, source)
        route = node(db, repository, target)
        cattr = call.get("attributes", {})
        rattr = route.get("attributes", {})
        alternatives = [p for p in cattr.get("paths", []) if target in (p.get("candidates") or [])]
        alt = alternatives[0] if alternatives else {}
        yield {
            "corpus": repository,
            "id": "%s -> %s" % (source, target),
            "call": source,
            "tier": tier_of(edge.get("confidence", 0), alt.get("match"), alt.get("origin")),
            "confidence": edge.get("confidence"),
            "call_site": line_of(call),
            "evidence_sites": [line_of(e) for e in edge.get("evidence", [])],
            "library": cattr.get("library"),
            "method": cattr.get("method"),
            "url_expression": cattr.get("url_expression"),
            "base": alt.get("base", cattr.get("base")),
            "base_expression": alt.get("base_expression", cattr.get("base_expression")),
            "wrapper": cattr.get("wrapper"),
            "linked_template": alt.get("template"),
            "match": alt.get("match"),
            "origin": alt.get("origin"),
            "scope": cattr.get("scope"),
            "route_site": line_of(route),
            "route_framework": rattr.get("framework"),
            "route_method": rattr.get("method"),
            "route_paths": [p.get("template") for p in rattr.get("paths", [])],
            "verdict": None,
            "url_built": None,
            "note": None,
        }


def export_unlinked(db, repository):
    rows = db.execute(
        "select payload from nodes where repository=? and kind='client_call' order by identity",
        (repository,),
    ).fetchall()
    for (payload,) in rows:
        call = json.loads(payload)
        attr = call.get("attributes", {})
        # A generated call (committed bundler output, ADR 0085) is not
        # linked by design: no recall miss.
        if attr.get("status") in ("linked", "wrapper", "test_only", "generated") or attr.get("scope") == "test":
            continue
        yield {
            "corpus": repository,
            "id": call["identity"],
            "area": call["file"].split("/")[0],
            "call_site": line_of(call),
            "library": attr.get("library"),
            "method": attr.get("method"),
            "status": attr.get("status"),
            "url_expression": attr.get("url_expression"),
            "templates": [p.get("template") for p in attr.get("paths", [])],
            "verdict": None,
            "note": None,
        }


def summary(db, repository):
    """Count stored client_call statuses (unknowns shown as counts)."""
    counts = {}
    for (status,) in db.execute(
        "select json_extract(payload, '$.attributes.status') from nodes where repository=? and kind='client_call'",
        (repository,),
    ):
        counts[status] = counts.get(status, 0) + 1
    return {"record": "summary", "corpus": repository, "status_counts": counts}


def sample(records, seed, per_stratum, by):
    rng = random.Random(seed)
    strata = {}
    for record in records:
        strata.setdefault(record.get(by) if by else "", []).append(record)
    chosen = []
    for key in sorted(strata, key=str):
        group = strata[key]
        if len(group) <= per_stratum:
            chosen.extend(group)
        else:
            chosen.extend(rng.sample(group, per_stratum))
    return chosen


def summarize(records):
    """Group adjudicated records.

    Returns (links, strata, recall, summaries): link verdict counts per
    (corpus, tier) plus (corpus, "all"); per (corpus, stratum) the population
    and the unlinked verdict counts; per corpus the number of production call
    sites with at least one correct link; and the stored status counts.
    """
    links, strata, recall, summaries = {}, {}, {}, {}
    for r in records:
        kind = r.get("record")
        if kind == "summary":
            summaries[r["corpus"]] = r
            continue
        if kind == "stratum":
            entry = strata.setdefault((r["corpus"], r["stratum"]), dict.fromkeys(UNLINKED_VERDICTS, 0))
            entry["population"] = r["population"]
            continue
        verdict = r.get("verdict")
        if verdict in VERDICTS:
            for key in ((r["corpus"], r["tier"]), (r["corpus"], "all")):
                counts = links.setdefault(key, dict.fromkeys(VERDICTS, 0))
                counts[verdict] += 1
            if verdict == "correct" and r.get("scope", "production") == "production":
                recall.setdefault(r["corpus"], set()).add(r["call"])
        elif verdict in UNLINKED_VERDICTS:
            entry = strata.setdefault((r["corpus"], r.get("stratum", "")), dict.fromkeys(UNLINKED_VERDICTS, 0))
            entry[verdict] += 1
        else:
            raise ValueError("record %s has no verdict" % r.get("call_site"))
    return links, strata, {k: len(v) for k, v in recall.items()}, summaries


def recall_estimate(linked, strata_rows):
    """Recall = linked / (linked + estimated missed calls).

    Missed calls are estimated per stratum as population x sampled missed
    share; the interval plugs in each stratum's Wilson bounds (conservative,
    the bounds are not simultaneous). A stratum adjudicated in full
    contributes its exact count.
    """
    point = low_missed = high_missed = 0.0
    for row in strata_rows:
        n = row["correctly_unlinked"] + row["missed"]
        if n == 0:
            continue
        population = row.get("population", n)
        lo, hi = wilson(row["missed"], n)
        if n >= population:  # a census of the stratum has no sampling error
            lo = hi = row["missed"] / n
        point += population * row["missed"] / n
        low_missed += population * lo
        high_missed += population * hi
    if linked + point == 0:
        return (0.0, 0.0, 0.0, 0.0)
    return (linked / (linked + point), linked / (linked + high_missed), linked / (linked + low_missed), point)


def pct(x):
    return "%.1f%%" % (100 * x)


def interval(k, n):
    lo, hi = wilson(k, n)
    return "%s [%s, %s]" % (pct(k / n) if n else "-", pct(lo), pct(hi))


def print_stats(records, out):
    links, strata, linked, summaries = summarize(records)
    out.write("| corpus | tier | links | correct | wrong | unverifiable | precision (of verifiable) | false-link rate (of all) | unverifiable share |\n")
    out.write("|---|---|---|---|---|---|---|---|---|\n")
    for (corpus, tier) in sorted(links):
        c = links[(corpus, tier)]
        n = sum(c.values())
        verifiable = c["correct"] + c["wrong"]
        out.write("| %s | %s | %d | %d | %d | %d | %s | %s | %s |\n" % (
            corpus, tier, n, c["correct"], c["wrong"], c["unverifiable"],
            interval(c["correct"], verifiable), interval(c["wrong"], n),
            interval(c["unverifiable"], n)))
    if strata:
        out.write("\n| corpus | stratum | unlinked calls | sampled | correctly unlinked | missed | missed share [Wilson 95%] |\n|---|---|---|---|---|---|---|\n")
        for (corpus, stratum) in sorted(strata):
            c = strata[(corpus, stratum)]
            n = c["correctly_unlinked"] + c["missed"]
            share = interval(c["missed"], n)
            if n >= c.get("population", n):
                share = "%s (all adjudicated)" % pct(c["missed"] / n)
            out.write("| %s | %s | %s | %d | %d | %d | %s |\n" % (
                corpus, stratum, c.get("population", n), n, c["correctly_unlinked"], c["missed"], share))
        out.write("\n| corpus | production calls with a correct link | estimated missed calls | recall estimate [conservative 95%] |\n|---|---|---|---|\n")
        for corpus in sorted({k[0] for k in strata}):
            rows = [strata[k] for k in strata if k[0] == corpus]
            point, lo, hi, missed = recall_estimate(linked.get(corpus, 0), rows)
            out.write("| %s | %d | %.0f | %s [%s, %s] |\n" % (corpus, linked.get(corpus, 0), missed, pct(point), pct(lo), pct(hi)))
    if summaries:
        out.write("\n| corpus | client calls | linked | dynamic | unmatched | ambiguous | wrapper | test_only | generated |\n|---|---|---|---|---|---|---|---|---|\n")
        for corpus in sorted(summaries):
            st = summaries[corpus]["status_counts"]
            out.write("| %s | %d | %s |\n" % (corpus, sum(st.values()), " | ".join(
                str(st.get(k, 0)) for k in ("linked", "dynamic", "unmatched", "ambiguous", "wrapper", "test_only", "generated"))))


def read_jsonl(paths):
    for path in paths:
        with open(path, encoding="utf-8") as f:
            for line in f:
                if line.strip():
                    yield json.loads(line)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    for name in ("export", "unlinked", "summary"):
        p = sub.add_parser(name)
        p.add_argument("store")
        p.add_argument("repository")
    p = sub.add_parser("sample")
    p.add_argument("file")
    p.add_argument("--seed", type=int, required=True)
    p.add_argument("--per-stratum", type=int, required=True)
    p.add_argument("--by", default=None)
    p = sub.add_parser("stats")
    p.add_argument("files", nargs="+")
    args = parser.parse_args(argv)

    if args.command == "summary":
        sys.stdout.write(json.dumps(summary(open_store(args.store), args.repository), sort_keys=True) + "\n")
    elif args.command in ("export", "unlinked"):
        db = open_store(args.store)
        produce = export_links if args.command == "export" else export_unlinked
        for record in produce(db, args.repository):
            sys.stdout.write(json.dumps(record, sort_keys=True) + "\n")
    elif args.command == "sample":
        for record in sample(list(read_jsonl([args.file])), args.seed, args.per_stratum, args.by):
            sys.stdout.write(json.dumps(record, sort_keys=True) + "\n")
    else:
        print_stats(list(read_jsonl(args.files)), sys.stdout)


if __name__ == "__main__":
    main()
