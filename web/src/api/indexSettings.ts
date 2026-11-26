// The settings comparison of an index (ADR 0088, rows ST-43..ST-46 of the
// settings claim contract): whether graphvein.yaml's index inputs changed
// since the last index run. It appears as freshness.settings of
// /api/v0/status and of each /api/v0/project member, and as index_settings
// of /api/v0/config. This module has no imports so every parser can use it.

/** SettingsState is the comparison's state. A state this UI does not know
 * reads as "unknown", so it is never shown as unchanged. */
export type SettingsState = "unchanged" | "changed" | "unknown" | "not_indexed";

export interface IndexSettings {
  state: SettingsState;
  /** The state as the server sent it. */
  reported_state: string;
  /** The keys a re-index would apply, in key-table order. */
  changed_keys: string[];
  /** Current keys the run's record does not hold. */
  unknown_keys: string[];
  reason: string | null;
}

const STATES: readonly string[] = ["unchanged", "changed", "unknown", "not_indexed"];

/** readIndexSettings checks a comparison object; fail builds the caller's
 * format error from a message. */
export function readIndexSettings(value: unknown, path: string, fail: (message: string) => Error): IndexSettings {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw fail(`${path} is not an object`);
  const o = value as Record<string, unknown>;
  const list = (key: string): string[] => {
    const v = o[key];
    if (v === undefined || v === null) return [];
    if (!Array.isArray(v) || !v.every((s) => typeof s === "string")) throw fail(`${path}.${key} is not a list of strings`);
    return v;
  };
  const reported = o.state;
  if (typeof reported !== "string") throw fail(`${path}.state is not a string`);
  const reason = o.reason;
  if (reason !== undefined && reason !== null && typeof reason !== "string") throw fail(`${path}.reason is not a string or null`);
  return {
    state: STATES.includes(reported) ? (reported as SettingsState) : "unknown",
    reported_state: reported,
    changed_keys: list("changed_keys"),
    unknown_keys: list("unknown_keys"),
    reason: typeof reason === "string" ? reason : null,
  };
}
