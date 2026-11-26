// Types, the hand-written guard and the request of the precision endpoint
// (docs/cli/serve.md "GET /api/v0/precision", T020, ADR 0066):
//   GET /api/v0/precision
// The rule table, whether the latest run may use it, and per-language
// counts of what the store holds. The UI shows these values as sent: it
// never decides a label, and it shows no percentage (O12).

import {
  ApiFormatError,
  array,
  getJSON,
  object,
  parseIndex,
  str,
  strOrNull,
  version,
  type IndexRun,
  type JSONObject,
  type PrecisionLabel,
  type Signal,
} from "@/api/endpoints";

/** PRECISION_PATH is the endpoint's path. */
export const PRECISION_PATH = "/api/v0/precision";

/** A test function named as evidence (contract or oracle). */
export interface EvidenceTest {
  package: string;
  name: string;
}

/** A quote from an ADR's review notes named as evidence. */
export interface EvidenceQuote {
  adr: string;
  quote: string;
}

/** Facts counts the facts this index holds in one family, by the
 * language of the file each is in ("none": no stored file record). */
export interface Facts {
  unit: string;
  total: number;
  by_language: Record<string, number>;
}

export interface PrecisionRule {
  id: string;
  kind: string;
  scope: string;
  /** What the row yields for this index; null when the word is unknown
   * to this UI (shown as no label). */
  label: PrecisionLabel | null;
  reason: string | null;
  languages: string[];
  facts: Facts | null;
  marker: string;
  match: { family: string | null; tools: string[]; dialects: string[] };
  evidence: {
    contract: EvidenceTest[];
    goldens: string[];
    corpus: EvidenceQuote[];
    review: EvidenceQuote[];
    oracle: EvidenceTest[];
    oracle_none: string | null;
  };
  refusal: string;
}

export interface DefaultFamily {
  family: string;
  label: PrecisionLabel | null;
  languages: string[];
  facts: Facts | null;
}

export interface LanguageFiles {
  language: string;
  total: number;
  /** Files by Graphvein's file label (run refusals applied). */
  by_label: Record<string, number>;
  /** An external run's files by the label the producer stored, not
   * checked by Graphvein; null for a built-in run. */
  reported_by_label: Record<string, number> | null;
}

export interface PrecisionResponse {
  api_version: number;
  repository: string;
  index: IndexRun | null;
  binary_extractor: string;
  run: { built_in: boolean; extractor_matches: boolean | null; refusal: string | null } | null;
  rules: PrecisionRule[];
  default: { row: string; label: PrecisionLabel | null; scope: string; covers: string; families: DefaultFamily[] };
  files: { reported_by: string | null; total: number; languages: LanguageFiles[] } | null;
}

function label(value: unknown): PrecisionLabel | null {
  return value === "precise" || value === "best_effort" ? value : null;
}

function count(value: unknown, path: string): number {
  if (typeof value !== "number" || !Number.isInteger(value) || value < 0) throw new ApiFormatError(`${path} is not a count`);
  return value;
}

function strings(value: unknown, path: string): string[] {
  const list = value === undefined || value === null ? [] : array(value, path);
  return list.map((s, i) => {
    if (typeof s !== "string") throw new ApiFormatError(`${path}[${String(i)}] is not a string`);
    return s;
  });
}

function counts(value: unknown, path: string): Record<string, number> {
  const out: Record<string, number> = {};
  for (const [k, v] of Object.entries(object(value, path))) out[k] = count(v, `${path}.${k}`);
  return out;
}

function facts(value: unknown, path: string): Facts | null {
  if (value === undefined || value === null) return null;
  const o = object(value, path);
  return { unit: str(o, "unit", path), total: count(o.total, `${path}.total`), by_language: counts(o.by_language, `${path}.by_language`) };
}

function tests(value: unknown, path: string): EvidenceTest[] {
  const list = value === undefined || value === null ? [] : array(value, path);
  return list.map((t, i) => {
    const p = `${path}[${String(i)}]`;
    const o = object(t, p);
    return { package: str(o, "package", p), name: str(o, "name", p) };
  });
}

function quotes(value: unknown, path: string): EvidenceQuote[] {
  const list = value === undefined || value === null ? [] : array(value, path);
  return list.map((t, i) => {
    const p = `${path}[${String(i)}]`;
    const o = object(t, p);
    return { adr: str(o, "adr", p), quote: str(o, "quote", p) };
  });
}

function optStr(obj: JSONObject, key: string, path: string): string | null {
  return obj[key] === undefined ? null : strOrNull(obj, key, path);
}

function parseRule(value: unknown, path: string): PrecisionRule {
  const o = object(value, path);
  const m = object(o.match, `${path}.match`);
  const e = object(o.evidence, `${path}.evidence`);
  return {
    id: str(o, "id", path),
    kind: str(o, "kind", path),
    scope: str(o, "scope", path),
    label: label(o.label),
    reason: optStr(o, "reason", path),
    languages: strings(o.languages, `${path}.languages`),
    facts: facts(o.facts, `${path}.facts`),
    marker: str(o, "marker", path),
    match: { family: optStr(m, "family", `${path}.match`), tools: strings(m.tools, `${path}.match.tools`), dialects: strings(m.dialects, `${path}.match.dialects`) },
    evidence: {
      contract: tests(e.contract, `${path}.evidence.contract`),
      goldens: strings(e.goldens, `${path}.evidence.goldens`),
      corpus: quotes(e.corpus, `${path}.evidence.corpus`),
      review: quotes(e.review, `${path}.evidence.review`),
      oracle: tests(e.oracle, `${path}.evidence.oracle`),
      oracle_none: optStr(e, "oracle_none", `${path}.evidence`),
    },
    refusal: str(o, "refusal", path),
  };
}

/** parsePrecision checks a /precision body. */
export function parsePrecision(value: unknown): PrecisionResponse {
  const p = "precision";
  const obj = object(value, p);
  const d = object(obj.default, `${p}.default`);
  let run: PrecisionResponse["run"] = null;
  if (obj.run !== null && obj.run !== undefined) {
    const r = object(obj.run, `${p}.run`);
    const matches = r.extractor_matches;
    if (typeof r.built_in !== "boolean") throw new ApiFormatError(`${p}.run.built_in is not a boolean`);
    if (matches !== null && typeof matches !== "boolean") throw new ApiFormatError(`${p}.run.extractor_matches is not a boolean or null`);
    run = { built_in: r.built_in, extractor_matches: matches, refusal: strOrNull(r, "refusal", `${p}.run`) };
  }
  let files: PrecisionResponse["files"] = null;
  if (obj.files !== null && obj.files !== undefined) {
    const f = object(obj.files, `${p}.files`);
    files = {
      reported_by: strOrNull(f, "reported_by", `${p}.files`),
      total: count(f.total, `${p}.files.total`),
      languages: array(f.languages, `${p}.files.languages`).map((l, i) => {
        const lp = `${p}.files.languages[${String(i)}]`;
        const o = object(l, lp);
        return {
          language: str(o, "language", lp),
          total: count(o.total, `${lp}.total`),
          by_label: counts(o.by_label, `${lp}.by_label`),
          reported_by_label: o.reported_by_label === undefined || o.reported_by_label === null ? null : counts(o.reported_by_label, `${lp}.reported_by_label`),
        };
      }),
    };
  }
  return {
    api_version: version(obj, p),
    repository: str(obj, "repository", p),
    index: parseIndex(obj.index, `${p}.index`),
    binary_extractor: str(obj, "binary_extractor", p),
    run,
    rules: array(obj.rules, `${p}.rules`).map((r, i) => parseRule(r, `${p}.rules[${String(i)}]`)),
    default: {
      row: str(d, "row", `${p}.default`),
      label: label(d.label),
      scope: str(d, "scope", `${p}.default`),
      covers: str(d, "covers", `${p}.default`),
      families: array(d.families, `${p}.default.families`).map((f, i) => {
        const fp = `${p}.default.families[${String(i)}]`;
        const o = object(f, fp);
        return { family: str(o, "family", fp), label: label(o.label), languages: strings(o.languages, `${fp}.languages`), facts: facts(o.facts, `${fp}.facts`) };
      }),
    },
    files,
  };
}

/** PrecisionLoader requests /precision; tests pass their own through
 * PrecisionLoaderContext. */
export type PrecisionLoader = (options: Signal) => Promise<PrecisionResponse>;

/** httpPrecisionLoader requests the server that served the page. */
export const httpPrecisionLoader: PrecisionLoader = (o) => getJSON(PRECISION_PATH, parsePrecision, o);
