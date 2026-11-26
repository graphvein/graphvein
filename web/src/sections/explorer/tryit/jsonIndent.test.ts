import { describe, expect, it } from "vitest";

import { indentJson } from "@/sections/explorer/tryit/jsonIndent";

describe("indentJson", () => {
  it("indents nested values and keeps empty containers compact", () => {
    expect(indentJson('{"a":{},"b":[],"c":[{"d":null}],"e":true}')).toEqual({
      ok: true,
      text: '{\n  "a": {},\n  "b": [],\n  "c": [\n    {\n      "d": null\n    }\n  ],\n  "e": true\n}',
      duplicateKeys: [],
    });
    expect(indentJson(" 42 ")).toEqual({ ok: true, text: "42", duplicateKeys: [] });
    expect(indentJson('"x"')).toEqual({ ok: true, text: '"x"', duplicateKeys: [] });
  });

  it("refuses anything that is not exactly one JSON value", () => {
    for (const bad of ["", "{", '{"a":1,}', "[1,]", "[1 2]", '{"a" 1}', "{1:2}", "01", "1.", "+1", "NaN", "tru", '"\\x"', '"a\tb"', "﻿{}", "{} {}", "[}", '{"a":1]', "[,1]", "{,}"]) {
      expect(indentJson(bad).ok, bad).toBe(false);
    }
  });

  it("names a repeated key once, comparing decoded keys", () => {
    const r = indentJson('[{"a":1,"\\u0061":2,"a":3},{"a":1}]');
    expect(r.ok && r.duplicateKeys).toEqual(["a"]);
  });

  it("re-indents up to 100 levels and leaves a deeper body as received", () => {
    expect(indentJson("[".repeat(100) + "]".repeat(100)).ok).toBe(true);
    expect(indentJson("[".repeat(101) + "]".repeat(101))).toEqual({ ok: false, reason: "nested deeper than 100 levels, too deep to re-indent" });
  });
});
