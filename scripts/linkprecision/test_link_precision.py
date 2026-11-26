#!/usr/bin/env python3
"""Offline checks for link_precision.py: a tiny in-memory store, no corpus.

Run: python3 -I scripts/linkprecision/test_link_precision.py
"""
import io
import json
from pathlib import Path
import runpy
import sqlite3
import unittest

lp = runpy.run_path(str(Path(__file__).with_name("link_precision.py")))


def store():
    db = sqlite3.connect(":memory:")
    db.execute("create table nodes (repository, identity, file, kind, payload)")
    db.execute("create table edges (repository, kind, source, target, payload)")
    call = {"identity": "c1", "file": "web/a.ts", "span": {"start_line": 3},
            "attributes": {"library": "fetch", "method": "GET", "status": "linked", "url_expression": "'/api/x'",
                           "base": "none", "wrapper": None,
                           "paths": [{"template": "/api/x", "match": "exact", "origin": None, "candidates": ["r1"]}]}}
    dyn = {"identity": "c2", "file": "docs/b.js", "span": {"start_line": 9},
           "attributes": {"library": "fetch", "method": None, "status": "dynamic", "url_expression": "url",
                          "paths": [{"template": None}]}}
    test = {"identity": "c3", "file": "web/a.test.ts", "span": {"start_line": 1}, "kind": "client_call",
            "attributes": {"status": "dynamic", "scope": "test", "paths": []}}
    route = {"identity": "r1", "file": "srv/r.go", "span": {"start_line": 7},
             "attributes": {"framework": "chi", "method": "GET", "paths": [{"template": "/api/x"}]}}
    for n, kind in ((call, "client_call"), (dyn, "client_call"), (test, "client_call"), (route, "route")):
        db.execute("insert into nodes values ('repo', ?, ?, ?, ?)", (n["identity"], n["file"], kind, json.dumps(n)))
    db.execute("insert into edges values ('repo', 'calls_route', 'c1', 'r1', ?)",
               (json.dumps({"confidence": 0.9, "evidence": [{"file": "web/a.ts", "span": {"start_line": 3}}]}),))
    return db


class LinkPrecisionTest(unittest.TestCase):
    def test_wilson(self):
        lo, hi = lp["wilson"](0, 10)
        self.assertEqual(lo, 0.0)
        self.assertAlmostEqual(hi, 0.2775, places=3)
        self.assertEqual(lp["wilson"](0, 0), (0.0, 0.0))

    def test_tiers(self):
        self.assertEqual(lp["tier_of"](0.9, "exact", None), "exact-0.9")
        self.assertEqual(lp["tier_of"](0.7, "exact", "https://h"), "exact-origin-0.7")
        self.assertEqual(lp["tier_of"](0.6, "suffix", None), "suffix-0.6")

    def test_export(self):
        db = store()
        links = list(lp["export_links"](db, "repo"))
        self.assertEqual(len(links), 1)
        self.assertEqual(links[0]["call_site"], "web/a.ts:3")
        self.assertEqual(links[0]["route_site"], "srv/r.go:7")
        self.assertEqual(links[0]["tier"], "exact-0.9")
        unlinked = list(lp["export_unlinked"](db, "repo"))
        self.assertEqual([u["id"] for u in unlinked], ["c2"])  # linked and test-scope calls excluded
        self.assertEqual(unlinked[0]["area"], "docs")
        self.assertEqual(lp["summary"](db, "repo")["status_counts"], {"linked": 1, "dynamic": 2})

    def test_sample_is_seeded_and_keeps_small_strata(self):
        records = [{"area": "a", "i": i} for i in range(50)] + [{"area": "b", "i": i} for i in range(3)]
        first = lp["sample"](records, 7, 10, "area")
        self.assertEqual(first, lp["sample"](records, 7, 10, "area"))
        self.assertEqual(sum(r["area"] == "a" for r in first), 10)
        self.assertEqual(sum(r["area"] == "b" for r in first), 3)

    def test_stats(self):
        records = [
            {"record": "stratum", "corpus": "c", "stratum": "s", "population": 4},
            {"record": "link", "corpus": "c", "tier": "exact-0.9", "verdict": "correct", "call": "x"},
            {"record": "link", "corpus": "c", "tier": "exact-0.9", "verdict": "wrong", "call": "y"},
            {"record": "unlinked", "corpus": "c", "stratum": "s", "verdict": "missed"},
            {"record": "unlinked", "corpus": "c", "stratum": "s", "verdict": "correctly_unlinked"},
        ]
        links, strata, linked, _ = lp["summarize"](records)
        self.assertEqual(links[("c", "all")], {"correct": 1, "wrong": 1, "unverifiable": 0})
        self.assertEqual(linked, {"c": 1})
        point, _, _, missed = lp["recall_estimate"](1, [strata[("c", "s")]])
        self.assertEqual(missed, 2.0)  # 4 unlinked calls x 1/2 missed
        self.assertAlmostEqual(point, 1 / 3)
        out = io.StringIO()
        lp["print_stats"](records, out)
        self.assertIn("| c | all | 2 | 1 | 1 | 0 |", out.getvalue())

    def test_missing_verdict_is_an_error(self):
        with self.assertRaises(ValueError):
            lp["summarize"]([{"record": "link", "corpus": "c", "tier": "t", "verdict": None}])


if __name__ == "__main__":
    unittest.main()
