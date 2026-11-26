// The guide's pages, parsed at build time from docs/guide/*.md (the
// graphvein-guide-markdown plugin in vite.config.ts). The glob is resolved
// by Vite when it builds the bundle: a new file in docs/guide becomes part
// of the page without a code change, in file-name order.

import type { GuidePage } from "@/sections/help/markdown";

const modules = import.meta.glob<GuidePage>("../../../../docs/guide/*.md", { query: "?guide", import: "default", eager: true });

/** GUIDE is every page of the guide, in file-name order. */
export const GUIDE: readonly GuidePage[] = Object.keys(modules)
  .sort((a, b) => a.localeCompare(b, "en"))
  .map((k) => modules[k])
  .filter((p): p is GuidePage => p !== undefined);
