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

    def test_area_recall(self):
        records = [
            # a sampled stratum wholly outside the areas: left out
            {"record": "stratum", "corpus": "c", "stratum": "docs", "population": 40},
            {"record": "unlinked", "corpus": "c", "stratum": "docs", "call_site": "docs/a.js:1", "verdict": "missed"},
            # a sampled stratum wholly inside: population x missed share
            {"record": "stratum", "corpus": "c", "stratum": "src", "population": 10},
            {"record": "unlinked", "corpus": "c", "stratum": "src", "call_site": "src/a.ts:1", "verdict": "missed"},
            {"record": "unlinked", "corpus": "c", "stratum": "src", "call_site": "src/b.ts:1", "verdict": "correctly_unlinked"},
            # a census that mixes areas: split exactly
            {"record": "stratum", "corpus": "c", "stratum": "mixed", "population": 2},
            {"record": "unlinked", "corpus": "c", "stratum": "mixed", "call_site": "src/c.ts:4", "verdict": "missed"},
            {"record": "unlinked", "corpus": "c", "stratum": "mixed", "call_site": "bin/d.mjs:4", "verdict": "missed"},
            {"record": "link", "corpus": "c", "tier": "exact-0.9", "verdict": "correct", "call": "x", "call_site": "src/x.ts:2"},
            {"record": "link", "corpus": "c", "tier": "exact-0.9", "verdict": "correct", "call": "y", "call_site": "docs/y.md:2"},
        ]
        linked, rows = lp["area_recall"](records, "c", ["src"])
        self.assertEqual(linked, 1)  # the docs link is outside
        point, lo, hi, missed = lp["recall_estimate"](linked, rows)
        self.assertEqual(missed, 6.0)  # src: 10 x 1/2, mixed census: 1
        self.assertAlmostEqual(point, 1 / 7)
        self.assertLess(lo, point)  # the src stratum is sampled
        out = io.StringIO()
        lp["print_stats"](records, out, {"c": ["src"]})
        self.assertIn("| c | `src/` | 1 | 6 |", out.getvalue())
        # only a census in the areas: no link, one exact miss
        linked, rows = lp["area_recall"](records, "c", ["bin"])
        self.assertEqual((linked, lp["recall_estimate"](linked, rows)[3]), (0, 1.0))

    def test_area_recall_refuses_a_mixed_sample(self):
        records = [
            {"record": "stratum", "corpus": "c", "stratum": "s", "population": 9},
            {"record": "unlinked", "corpus": "c", "stratum": "s", "call_site": "src/a.ts:1", "verdict": "missed"},
            {"record": "unlinked", "corpus": "c", "stratum": "s", "call_site": "docs/a.js:1", "verdict": "missed"},
        ]
        with self.assertRaises(ValueError):
            lp["area_recall"](records, "c", ["src"])

    def test_missing_verdict_is_an_error(self):
        with self.assertRaises(ValueError):
            lp["summarize"]([{"record": "link", "corpus": "c", "tier": "t", "verdict": None}])


if __name__ == "__main__":
    unittest.main()
