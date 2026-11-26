// Types, the hand-written guard and the request of the code peek source
// endpoint (docs/cli/serve.md "GET /api/v0/source", ADR 0044):
//   GET /api/v0/source?id=&context=
// The highlight comes only from the server: the UI never computes a span.
// A state this UI does not know is kept raw and treated as "no highlight,
// show the state".

import {
  ApiFormatError,
  array,
  bool,
  getJSON,
  intOrNull,
  object,
  parseIdeStatus,
  parseIndex,
  parseFileLabel,
  parseNode,
  parseScopeLabel,
  str,
  strOrNull,
  version,
  type FileLabel,
  type IdeStatus,
  type IndexRun,
  type PresentedNode,
  type ScopeLabel,
  type Signal,
} from "@/api/endpoints";

/** The states the server documents. Any other string is possible from a
 * newer server and must be handled as "no highlight". */
export const SOURCE_STATES = [
  "exact",
  "text_match",
  "changed",
  "span_invalid",
  "missing",
  "not_regular",
  "too_large",
  "binary",
  "unreadable",
  "no_location",
  "not_indexed",
] as const;

export type KnownSourceState = (typeof SOURCE_STATES)[number];

/** The only states whose highlight the UI draws (ADR 0044 decision 2). */
export const HIGHLIGHT_STATES: readonly string[] = ["exact", "text_match"];

/** isKnownState says whether the server's state is one this UI documents. */
export function isKnownState(state: string): state is KnownSourceState {
  return (SOURCE_STATES as readonly string[]).includes(state);
}

/** SourceHighlight is a range of the returned lines: 1-based lines,
 * 0-based UTF-16 code unit columns within the returned line texts, end
 * exclusive. */
export interface SourceHighlight {
  start_line: number;
  start_column: number;
  end_line: number;
  end_column: number;
}

export interface SourceView {
  state: string;
  reason: string | null;
  file: { path: string; size: number | null; line_count: number | null; label: FileLabel };
  first_line: number | null;
  lines: string[];
  cut_lines: number[];
  truncated: boolean;
  /** Null unless state is exact or text_match; the guard also drops it for
   * any other state, so a view can never draw it. */
  highlight: SourceHighlight | null;
}

export interface SourceResponse {
  api_version: number;
  repository: string;
  index: IndexRun | null;
  ide: IdeStatus;
  node: PresentedNode;
  source: SourceView;
  /** T020 O-S1-5: the code calls label of a function or method, whose
   * calls are shown next to the file label (which never covers them);
   * null for any other node or an older server. */
  code_calls_precision: ScopeLabel | null;
}

function int(obj: Record<string, unknown>, key: string, path: string): number {
  const v = obj[key];
  if (typeof v !== "number" || !Number.isInteger(v)) throw new ApiFormatError(`${path}.${key} is not an integer`);
  return v;
}

function parseHighlight(value: unknown, path: string): SourceHighlight | null {
  if (value === null || value === undefined) return null;
  const obj = object(value, path);
  const h = {
    start_line: int(obj, "start_line", path),
    start_column: int(obj, "start_column", path),
    end_line: int(obj, "end_line", path),
    end_column: int(obj, "end_column", path),
  };
  if (h.start_line < 1 || h.start_column < 0 || h.end_column < 0 || h.end_line < h.start_line) {
    throw new ApiFormatError(`${path} is not a forward range`);
  }
  if (h.end_line === h.start_line && h.end_column < h.start_column) {
    throw new ApiFormatError(`${path} ends before it starts`);
  }
  return h;
}

/** parseSource checks a source endpoint body. */
export function parseSource(value: unknown): SourceResponse {
  const p = "source";
  const obj = object(value, p);
  const apiVersion = version(obj, p);
  const s = object(obj.source, `${p}.source`);
  const f = object(s.file, `${p}.source.file`);
  const lines = array(s.lines, `${p}.source.lines`).map((l, i) => {
    if (typeof l !== "string") throw new ApiFormatError(`${p}.source.lines[${String(i)}] is not a string`);
    return l;
  });
  const cut = array(s.cut_lines, `${p}.source.cut_lines`).map((n, i) => {
    if (typeof n !== "number" || !Number.isInteger(n)) throw new ApiFormatError(`${p}.source.cut_lines[${String(i)}] is not an integer`);
    return n;
  });
  const state = str(s, "state", `${p}.source`);
  const highlight = parseHighlight(s.highlight, `${p}.source.highlight`);
  const firstLine = intOrNull(s, "first_line", `${p}.source`);
  if (lines.length > 0 && firstLine === null) throw new ApiFormatError(`${p}.source.first_line is null but lines are present`);
  return {
    api_version: apiVersion,
    repository: str(obj, "repository", p),
    index: parseIndex(obj.index, `${p}.index`),
    ide: parseIdeStatus(obj.ide, `${p}.ide`),
    node: parseNode(obj.node, `${p}.node`),
    source: {
      state,
      reason: strOrNull(s, "reason", `${p}.source`),
      file: {
        path: str(f, "path", `${p}.source.file`),
        size: intOrNull(f, "size", `${p}.source.file`),
        line_count: intOrNull(f, "line_count", `${p}.source.file`),
        label: parseFileLabel(f, `${p}.source.file`),
      },
      first_line: firstLine,
      lines,
      cut_lines: cut,
      truncated: bool(s, "truncated", `${p}.source`),
      // Only exact and text_match may be drawn; a highlight next to any
      // other state (a server bug, or a future state) is dropped.
      highlight: HIGHLIGHT_STATES.includes(state) ? highlight : null,
    },
    code_calls_precision: parseScopeLabel(obj.code_calls_precision, `${p}.code_calls_precision`),
  };
}

/** The context the server uses when none is given, and its maximum. */
export const DEFAULT_CONTEXT = 8;
export const MAX_CONTEXT = 50;

export interface SourceParams {
  id: string;
  context: number;
  /** The editor chosen in this browser (ADR 0050); absent: none. */
  editor?: string;
}

/** sourcePath builds the request path. */
export function sourcePath(p: SourceParams): string {
  const editor = p.editor !== undefined ? `&editor=${encodeURIComponent(p.editor)}` : "";
  return `/api/v0/source?id=${encodeURIComponent(p.id)}&context=${String(p.context)}${editor}`;
}

/** SourceClient is what the code peek requests; tests pass their own. */
export interface SourceClient {
  source(p: SourceParams, options: Signal): Promise<SourceResponse>;
}

/** httpSourceClient requests the server that served the page;
 * index_changed is retried once as for the API section. */
export const httpSourceClient: SourceClient = {
  source: (p, o) => getJSON(sourcePath(p), parseSource, o),
};
