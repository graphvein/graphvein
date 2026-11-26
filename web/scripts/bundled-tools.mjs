// Build tools whose own code ends up in the built bundle (ADR 0041,
// decision 4). They are development packages in package-lock.json, but the
// output carries their code, so check-licences.mjs holds them to the
// runtime allow-list and third-party-licenses.mjs writes their notices.
//
// Each entry names markers: text that only that tool puts into the output.
// third-party-licenses.mjs fails when a marker of a tool in KNOWN_TOOLS is
// in the build but the tool is not in BUNDLED_TOOLS, and when a listed tool
// leaves no marker, so the list cannot drift from the build silently.
// Tools that only transform code without adding their own (TypeScript,
// @vitejs/plugin-react in production, the CSS minifier lightningcss) are
// not listed.

/** Tools whose code is in the bundle today, by package name. */
export const BUNDLED_TOOLS = ["rolldown", "tailwindcss", "tw-animate-css", "vite"];

/**
 * Every tool known to inject code, with what it injects, its markers and
 * any licence files beyond LICENSE/COPYING that cover the injected code.
 */
export const KNOWN_TOOLS = [
  {
    name: "rolldown",
    injects: "CommonJS interop helpers (__toESM and friends, derived from esbuild)",
    markers: [{ ext: ".js", pattern: /Object\.create,\w+=Object\.defineProperty/ }],
    extraFiles: ["THIRD-PARTY-LICENSE"],
  },
  {
    name: "tailwindcss",
    injects: "preflight, theme variables and utility CSS",
    markers: [{ ext: ".css", pattern: /\/\*! tailwindcss v/ }],
    extraFiles: [],
  },
  {
    name: "tw-animate-css",
    injects: "enter/exit animation keyframes and utilities",
    markers: [{ ext: ".css", pattern: /--tw-enter-opacity/ }],
    extraFiles: [],
  },
  {
    // Since T016f2's lazy Flow chunk: the helper that wraps dynamic
    // imports and loads a lazy chunk's CSS. Its csp-nonce lookup is in no
    // other tool's output. The modulepreload polyfill stays disabled
    // (vite.config.ts); its marker is not checked separately, since the
    // helper contains the same feature test.
    name: "vite",
    injects: "preload helper for the lazy Flow chunk (__vitePreload)",
    markers: [{ ext: ".js", pattern: /meta\[property=csp-nonce\]/ }],
    extraFiles: [],
  },
];
