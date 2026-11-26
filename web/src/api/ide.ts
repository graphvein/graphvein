// Types, guard and request of the editor preset list (docs/cli/serve.md
// "GET /api/v0/ide", ADR 0050). The editor picker reads it, so the preset
// table lives only in Go (internal/config/ide.go).

import { ApiFormatError, array, bool, getJSON, object, str, strOrNull, version, type Signal } from "@/api/endpoints";

/** IDE_PATH is the preset list endpoint. */
export const IDE_PATH = "/api/v0/ide";

/** How far a preset was verified. Another string from a newer server is
 * kept and shown as "verification unknown", never as verified. */
export type PresetStatus = "verified" | "partly_verified" | "limited" | (string & {});

export interface EditorPreset {
  editor: string;
  label: string;
  status: PresetStatus;
  note: string;
  /** The note's caveat in a few words ("macOS only") for a compact row;
   * "" for a verified preset and from a server that does not send it. */
  short_note: string;
}

/** The team default exactly as graphvein.yaml gives it. */
export interface RepositoryIde {
  available: boolean;
  error: string | null;
  editor: string | null;
  label: string | null;
  custom: boolean;
  source: string;
}

export interface IdeInfo {
  api_version: number;
  presets: EditorPreset[];
  repository: RepositoryIde;
}

/** parseIdeInfo checks a /api/v0/ide body. */
export function parseIdeInfo(value: unknown): IdeInfo {
  const p = "ide";
  const obj = object(value, p);
  const presets = array(obj.presets, `${p}.presets`).map((v, i) => {
    const q = `${p}.presets[${String(i)}]`;
    const o = object(v, q);
    return { editor: str(o, "editor", q), label: str(o, "label", q), status: str(o, "status", q), note: str(o, "note", q), short_note: typeof o.short_note === "string" ? o.short_note : "" };
  });
  const seen = new Set<string>();
  for (const preset of presets) {
    if (seen.has(preset.editor)) throw new ApiFormatError(`${p}.presets repeats ${preset.editor}`);
    seen.add(preset.editor);
  }
  const r = object(obj.repository, `${p}.repository`);
  const q = `${p}.repository`;
  return {
    api_version: version(obj, p),
    presets,
    repository: {
      available: bool(r, "available", q),
      error: strOrNull(r, "error", q),
      editor: strOrNull(r, "editor", q),
      label: strOrNull(r, "label", q),
      custom: bool(r, "custom", q),
      source: str(r, "source", q),
    },
  };
}

/** IdeFetcher loads the preset list; tests pass their own. */
export type IdeFetcher = (options: Signal) => Promise<IdeInfo>;

/** fetchIdeInfo requests the server that served the page. */
export const fetchIdeInfo: IdeFetcher = (o) => getJSON(IDE_PATH, parseIdeInfo, o);
