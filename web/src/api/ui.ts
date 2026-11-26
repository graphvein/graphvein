// Types, guard and request of the UI defaults the repository sets in
// graphvein.yaml (docs/cli/serve.md "GET /api/v0/ui", T025b).

import { getJSON, object, strOrNull, version, type Signal } from "@/api/endpoints";

/** UI_PATH is the UI defaults endpoint. */
export const UI_PATH = "/api/v0/ui";

/** UiDefaults is the body of GET /api/v0/ui. labels is the repository's
 * ui.labels as sent: "plain", "technical", another string from a newer
 * server (ignored by the UI), or null when none is set. impact_depth and
 * chain_depth are ui.impact_depth and ui.chain_depth (T042), null when
 * none is set (or absent from a server before T042). A depth field that is
 * not an integer (a newer server's other form) is read as absent, so it
 * never discards labels; a value outside 1 to 10 is ignored by the UI. */
export interface UiDefaults {
  api_version: number;
  labels: string | null;
  impact_depth?: number | null;
  chain_depth?: number | null;
  error: string | null;
  /** The public wrong-fact issue form the binary was built with (ADR
   * 0089), or null: while null the UI renders no "Report a wrong fact"
   * flag. An older server omits it (null). */
  report_url?: string | null;
}

/** parseUiDefaults checks a /api/v0/ui body. */
export function parseUiDefaults(value: unknown): UiDefaults {
  const p = "ui";
  const obj = object(value, p);
  // A depth field alone falls back to absent (the built-in or browser depth
  // applies) instead of failing the whole answer: labels stay usable.
  const depth = (key: string): number | null => {
    const v = obj[key];
    return typeof v === "number" && Number.isInteger(v) ? v : null;
  };
  const report = obj.report_url === undefined ? null : strOrNull(obj, "report_url", p);
  return {
    api_version: version(obj, p),
    labels: strOrNull(obj, "labels", p),
    impact_depth: depth("impact_depth"),
    chain_depth: depth("chain_depth"),
    error: strOrNull(obj, "error", p),
    // Only an https URL is ever opened; anything else reads as no switch.
    report_url: report !== null && report.startsWith("https://") ? report : null,
  };
}

/** UiFetcher loads the UI defaults; tests pass their own. */
export type UiFetcher = (options: Signal) => Promise<UiDefaults>;

/** fetchUiDefaults requests the server that served the page. */
export const fetchUiDefaults: UiFetcher = (o) => getJSON(UI_PATH, parseUiDefaults, o);
