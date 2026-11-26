// The Go server's status goldens (internal/server/testdata), imported so the
// Go and TypeScript sides cannot drift apart: a changed golden changes what
// these tests parse.
import fresh from "../../../internal/server/testdata/status-fresh.golden.json";
import notIndexed from "../../../internal/server/testdata/status-not-indexed.golden.json";
import stale from "../../../internal/server/testdata/status-stale.golden.json";
import unknown from "../../../internal/server/testdata/status-unknown.golden.json";

import { parseStatus, type Status } from "@/api/status";

export const GOLDENS = {
  fresh,
  stale,
  not_indexed: notIndexed,
  unknown,
} as const;

/** goldenStatus returns a parsed copy of a golden that a test may modify. */
export function goldenStatus(name: keyof typeof GOLDENS): Status {
  return parseStatus(structuredClone(GOLDENS[name]));
}

/** rawGolden returns a mutable deep copy of a golden's JSON. */
export function rawGolden(name: keyof typeof GOLDENS): Record<string, unknown> {
  return structuredClone(GOLDENS[name]);
}
