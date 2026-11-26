import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig, type Plugin } from "vitest/config";

import { GuideSyntaxError, parseGuidePage } from "./src/sections/help/markdown.ts";

// The Help page's text is the public Markdown in docs/guide (T045). Each
// file is imported as "<file>.md?guide" (src/sections/help/guide.ts); this
// plugin turns the Markdown that Vite's loader read into the parsed page,
// so the bundle carries no Markdown parser and a form the parser does not
// accept fails the build with the file and line.
function guideMarkdown(): Plugin {
  return {
    name: "graphvein-guide-markdown",
    transform(code, id) {
      const [file = "", query = ""] = id.split("?");
      if (query !== "guide" || !file.endsWith(".md")) return null;
      const name = file.slice(file.lastIndexOf("/") + 1);
      try {
        return { code: `export default ${JSON.stringify(parseGuidePage(name, code))};`, map: null };
      } catch (err) {
        if (err instanceof GuideSyntaxError) this.error(err.message);
        throw err;
      }
    },
  };
}

// The build writes straight into the Go embed directory; the output is
// committed so `go build` needs no Node (ADR 0041). scripts/check-web.sh
// rebuilds it and fails when the committed files differ.
export default defineConfig({
  plugins: [react(), tailwindcss(), guideMarkdown()],
  resolve: {
    // "@/..." imports come from the paths entry in tsconfig.json.
    tsconfigPaths: true,
  },
  build: {
    outDir: "../internal/server/ui/static",
    // Required because outDir is outside the project root; the directory
    // holds only generated files.
    emptyOutDir: true,
    assetsDir: "assets",
    sourcemap: false,
    // The Flow tab is a lazy chunk (T016f D10). Vite wraps every dynamic
    // import in its preload helper, which loads the chunk's own CSS with a
    // <link> (allowed by style-src 'self'); the helper's code is in the
    // entry chunk and listed in scripts/bundled-tools.mjs. Preloading the
    // lazy chunk's JS dependencies is off: it has none beyond the entry
    // chunk, and the modulepreload polyfill stays out of the bundle.
    modulePreload: false,
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    // The workspace (web/), the claim contracts, which the Explorer's row
    // test reads as text (T029), and the Help page's guide (T045). Nothing
    // else outside web/ is served.
    fs: { allow: [".", "../docs/contract/claims", "../docs/guide"] },
    proxy: {
      // changeOrigin rewrites Host to 127.0.0.1:7420, which the Go server's
      // Host allow-list accepts.
      "/api": { target: "http://127.0.0.1:7420", changeOrigin: true },
    },
  },
  test: {
    environment: "jsdom",
    env: { TZ: "UTC" },
    setupFiles: ["./src/test/setup.ts"],
  },
});
