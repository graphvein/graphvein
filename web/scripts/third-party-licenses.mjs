// Writes internal/server/ui/static/third-party-licenses.txt: the licence
// notices of every runtime package in package-lock.json, of the build tools
// whose code is in the bundle (bundled-tools.mjs), of the shadcn/ui
// components copied into src/components/ui and of the editor logos in
// src/assets/editor-logos (ADR 0050). The minifier drops licence
// comments from the bundle, so the binary serves this file instead
// (ADR 0041). Output is deterministic: packages sorted by name, LF endings,
// no dates or absolute paths. `npm run build` runs it as its postbuild
// step, after Vite has emptied the output directory.
//
// It first checks the built assets against KNOWN_TOOLS and fails when the
// bundle contains code of a tool that BUNDLED_TOOLS does not list, or when
// a listed tool left no trace.
import { createHash } from "node:crypto";
import { existsSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { extname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { BUNDLED_TOOLS, KNOWN_TOOLS } from "./bundled-tools.mjs";

const webDir = fileURLToPath(new URL("..", import.meta.url));
const outDir = join(webDir, "..", "internal", "server", "ui", "static");
const outFile = join(outDir, "third-party-licenses.txt");

// goToolchainVersion returns the Go release that builds the binary: the
// toolchain directive of go.mod (for example "toolchain go1.27.4" -> "1.27.4"),
// or its go directive when there is no toolchain line. The notice names the
// standard library version from this one source so it cannot drift from the
// toolchain; internal/server/ui's TestGoNoticeVersion checks the committed
// file against go.mod.
function goToolchainVersion() {
  const goMod = readFileSync(join(webDir, "..", "go.mod"), "utf8");
  const toolchain = /^toolchain go(\S+)\s*$/m.exec(goMod);
  if (toolchain !== null) return toolchain[1];
  const goLine = /^go (\S+)\s*$/m.exec(goMod);
  if (goLine !== null) return goLine[1];
  console.error("go.mod has neither a toolchain nor a go directive (web/scripts/third-party-licenses.mjs)");
  process.exit(1);
}

// Copied verbatim from LICENSE.md of shadcn-ui/ui at commit
// 4aaea19d9cff3e0908e12a8c1d504b8c5ec34610 (see src/components/ui/README.md).
const SHADCN_NOTICE = `MIT License

Copyright (c) 2023 shadcn

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`;

// Copied verbatim from LICENSE of the Go distribution (unchanged across the
// 1.27 releases). The binary
// links the Go standard library, and internal/extract/routes/gohttp_mux.go
// transcribes net/http's pattern code (T023), so the served notice carries
// Go's licence too.
const GO_NOTICE = `Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
`;

// Statements printed before a package's licence text. elkjs is dual
// licensed ("EPL-2.0 OR GPL-3.0-or-later"); ADR 0046 takes it under
// EPL-2.0, ships it unmodified and records where its source is. The
// version is part of the key: check-licences.mjs accepts only this version,
// and a new one needs a new review of this text too.
const NOTICES = {
  "elkjs@0.12.0": `Graphvein distributes elkjs 0.12.0 under the Eclipse Public License 2.0
(the package is offered under "EPL-2.0 OR GPL-3.0-or-later"; Graphvein
chooses EPL-2.0). It is distributed unmodified, in object code form: the
web UI serves the package file lib/elk-worker.min.js byte for byte as
/assets/elk-worker.min-<hash>.js, a separate file loaded as a web worker
only when the Flow tab opens. No other elkjs file is in the bundle.

Source code: elkjs 0.12.0 is available from
  https://github.com/kieler/elkjs/tree/0.12.0
  https://registry.npmjs.org/elkjs/-/elkjs-0.12.0.tgz
(the tag holds the same lib/elk-worker.min.js). It is compiled from the
Eclipse Layout Kernel (ELK); elkjs's release procedure builds a release
from the matching ELK release tag, here
  https://github.com/eclipse-elk/elk/tree/v0.12.0
The source code is available under the EPL-2.0 text below.
`,
};

// The editor logos bundled from src/assets/editor-logos (ADR 0050,
// the T016k task record (internal)). They are not npm packages, so
// check-licences.mjs does not see them: this list is their licence record,
// and the build fails when a file there is not listed here or its bytes
// differ from the recorded hash (a changed logo needs a new review).
// Attribution goes on this page only, never on the main UI pages.
const EDITOR_LOGOS = [
  {
    file: "cursor-cube-2d-dark.svg",
    sha256: "cd0e3e5d8991a4cdd4577f8896cd063105207665165c73e25a1ff918dd367eb7",
    // Recorded with the light file below.
    text: null,
  },
  {
    file: "cursor-cube-2d-light.svg",
    sha256: "c483c02f78eb2619778fdd959e72a9adfac4844854472cd2653d4cbfd60e4d71",
    text: `Cursor logo, 2D cube: cursor-cube-2d-light.svg (for light backgrounds)
  and cursor-cube-2d-dark.svg (for dark backgrounds), unaltered
Source: https://cursor.com/brand ("Download brand assets",
  https://ptht05hbb1ssoooe.public.blob.vercel-storage.com/assets/brand/cursor-brand-assets.zip,
  files General Logos/Cube/SVG/CUBE_2D_LIGHT.svg and CUBE_2D_DARK.svg)
Terms: https://cursor.com/brand - "Resources to represent Cursor
  consistently and accurately."; no licence, permitted-use or trademark
  text on the page or in the package. Shown only on "Open in Cursor" links
  and the editor picker, unaltered, in the 2D form the page names as the
  default; no affiliation with or endorsement by Anysphere, Inc. is implied.
Cursor is a product of Anysphere, Inc.
`,
  },
  {
    file: "vscode.svg",
    sha256: "74ad401c6487a0dc5af1f6b2935556ab2d3553ae45effc7e1697b4d117d046b6",
    text: `Visual Studio Code icon (vscode.svg), unaltered
Source: https://code.visualstudio.com/assets/branding/visual-studio-code-icons.zip
  (archive version 2021-06-21, file visual-studio-code-icons/vscode.svg)
Terms: https://code.visualstudio.com/brand - shown only on "Open in VS Code"
  links and the editor picker, as the blue icon, unmodified, with clear
  space of at least 25% around it, with nothing added to or over the icon
  or its clear space. Not used to identify or promote
  Graphvein, and no association with Microsoft is implied.
Visual Studio Code, VS Code, and the Visual Studio Code icon are trademarks
of Microsoft Corporation. All rights reserved.
`,
  },
  {
    file: "intellij-idea.svg",
    sha256: "5ee273a204c3c5b3a33a39269c6cefd87890bdafa64797580daeaf8ea32346c7",
    text: `IntelliJ IDEA product icon (intellij-idea.svg), unaltered
Source: https://github.com/JetBrains/logos (web/intellij-idea/intellij-idea.svg);
  the brand page download https://resources.jetbrains.com/storage/logos/web/intellij-idea/intellij-idea.svg
  could not be fetched when the file was added.
Licence: https://github.com/JetBrains/logos/blob/master/LICENSE.txt - the
  JetBrains logos "are not part of the Apache 2.0 license". Third parties may
  use them only under its conditions: only to point to JetBrains (linked to
  https://www.jetbrains.com/); as a secondary brand, less prominent than
  Graphvein's own branding; unaltered, in original colours, and following
  JetBrains' brand and trademark guidelines; use on a product sold
  commercially needs JetBrains' explicit permission (marketing@jetbrains.com),
  not yet asked.
Terms: https://www.jetbrains.com/company/brand/ - JetBrains Brand Assets may
  be used to advertise built-in integration with a JetBrains product; shown
  unaltered except for size, never below 24 px, not combined with other
  symbols (no marker or badge on the icon or in its clear space of half the
  black square's width), and less prominent than Graphvein's own branding.
  No affiliation with JetBrains is implied.
IntelliJ IDEA and the IntelliJ IDEA logo are trademarks of JetBrains s.r.o.
https://www.jetbrains.com/
`,
  },
  {
    file: "zed-logomark-black.png",
    sha256: "63932eb5ca704adabd2159d1d4511879f1f2e2d02678cc61eeb4dedaf96e732e",
    text: `Zed logomark: zed-logomark-black.png (full black) and
  zed-logomark-white.png (full white, for the dark theme)
Source: https://zed.dev/brand ("Logomark" downloads
  logo_wordmark_black_bigger.png and logo_wordmark_white_bigger.png),
  scaled proportionally from 1012 to 128 px, colours and shape unchanged.
Terms: https://zed.dev/brand - the logo is used only in brand blue, full
  white or full black, without distortion, and implies no official
  connection with or endorsement by Zed Industries, Inc.
The Zed name and logos are trademarks of Zed Industries, Inc.
`,
  },
  {
    file: "zed-logomark-white.png",
    sha256: "e2cf0cbe7c00fa89ed4e3d5fae6f3d0b9997d5674416a10a0928e3276380722c",
    // Recorded with the black file above.
    text: null,
  },
];

const LICENCE_FILE = /^(licen[cs]e|copying)/i;

function normalise(text) {
  return text.replace(/\r\n?/g, "\n").replace(/\s+$/, "") + "\n";
}

function licenceText(dir, extraFiles = []) {
  const names = readdirSync(dir).filter((n) => LICENCE_FILE.test(n)).sort().concat(extraFiles);
  return names.map((n) => normalise(readFileSync(join(dir, n), "utf8"))).join("\n");
}

// Which known tools left code in the built assets.
const assetsDir = join(outDir, "assets");
if (!existsSync(assetsDir)) {
  console.error("internal/server/ui/static/assets is missing; run npm run build first");
  process.exit(1);
}
const assets = readdirSync(assetsDir)
  .sort()
  .map((n) => ({ ext: extname(n), text: readFileSync(join(assetsDir, n), "utf8") }));
const toolFailures = [];
for (const tool of KNOWN_TOOLS) {
  const found = tool.markers.some((m) => assets.some((a) => a.ext === m.ext && m.pattern.test(a.text)));
  const listed = BUNDLED_TOOLS.includes(tool.name);
  if (found && !listed) {
    toolFailures.push(`${tool.name}: the bundle contains its ${tool.injects}; add it to BUNDLED_TOOLS`);
  } else if (!found && listed) {
    toolFailures.push(`${tool.name}: listed in BUNDLED_TOOLS but the bundle has no marker of it`);
  }
}
for (const name of BUNDLED_TOOLS) {
  if (!KNOWN_TOOLS.some((t) => t.name === name)) toolFailures.push(`${name}: in BUNDLED_TOOLS without markers`);
}
if (toolFailures.length > 0) {
  console.error("Bundled tool check failed (web/scripts/bundled-tools.mjs):");
  for (const f of toolFailures) console.error(`  ${f}`);
  process.exit(1);
}

const lock = JSON.parse(readFileSync(join(webDir, "package-lock.json"), "utf8"));
const packages = [];
for (const [key, entry] of Object.entries(lock.packages ?? {})) {
  // Runtime packages only: dev tools and devOptional type packages are not
  // part of the bundle.
  if (key === "" || entry.dev === true || entry.devOptional === true) continue;
  const name = key.slice(key.lastIndexOf("node_modules/") + "node_modules/".length);
  const dir = join(webDir, key);
  if (!existsSync(dir)) {
    console.error(`${name}: ${key} is missing; run npm ci first`);
    process.exit(1);
  }
  packages.push({ name, version: entry.version, licence: entry.license, dir });
}
for (const name of BUNDLED_TOOLS) {
  const key = `node_modules/${name}`;
  const entry = lock.packages?.[key];
  const dir = join(webDir, key);
  if (entry === undefined || !existsSync(dir)) {
    console.error(`${name}: ${key} is missing; run npm ci first`);
    process.exit(1);
  }
  const tool = KNOWN_TOOLS.find((t) => t.name === name);
  packages.push({ name, version: entry.version, licence: entry.license, dir, tool });
}
packages.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : a.version < b.version ? -1 : 1));

const rule = "-".repeat(72);
let out =
  "Third-party software in the Graphvein web UI\n\n" +
  "The web UI embedded in this binary includes the following software:\n" +
  "its runtime packages and the parts of build tools that the build copies\n" +
  "into the output.\n" +
  "Generated by web/scripts/third-party-licenses.mjs from web/package-lock.json.\n" +
  "Graphvein's own notice and the notices of the Go modules and tree-sitter\n" +
  "grammars in the binary are served at /NOTICE.txt and /THIRD_PARTY_NOTICES.txt.\n";

out += `\n${rule}\nshadcn/ui components (button, badge, tooltip, separator, skeleton)\n`;
out += "Source: https://github.com/shadcn-ui/ui commit 4aaea19d9cff3e0908e12a8c1d504b8c5ec34610\nLicence: MIT\n\n";
out += SHADCN_NOTICE;

for (const p of packages) {
  out += `\n${rule}\n${p.name} ${p.version}\nLicence: ${p.licence}\n`;
  if (p.tool !== undefined) out += `Build tool; the bundle contains its ${p.tool.injects}.\n`;
  out += "\n";
  const notice = NOTICES[`${p.name}@${p.version}`];
  if (notice !== undefined) out += `${notice}\n`;
  const text = licenceText(p.dir, p.tool?.extraFiles);
  if (text !== "") {
    out += text;
  } else {
    const meta = JSON.parse(readFileSync(join(p.dir, "package.json"), "utf8"));
    const author = typeof meta.author === "string" ? meta.author : meta.author?.name;
    out += `The package ships no licence file. Its package.json declares ${p.licence}` +
      (author ? `, author ${author}` : "") + ".\n";
  }
}

const logoDir = join(webDir, "src", "assets", "editor-logos");
const logoFiles = readdirSync(logoDir).filter((n) => n !== "README.md").sort();
const logoFailures = [];
for (const name of logoFiles) {
  const entry = EDITOR_LOGOS.find((l) => l.file === name);
  if (entry === undefined) {
    logoFailures.push(`${name}: in src/assets/editor-logos but not in EDITOR_LOGOS`);
    continue;
  }
  const hash = createHash("sha256").update(readFileSync(join(logoDir, name))).digest("hex");
  if (hash !== entry.sha256) logoFailures.push(`${name}: sha256 ${hash} differs from the reviewed ${entry.sha256}`);
}
for (const l of EDITOR_LOGOS) {
  if (!logoFiles.includes(l.file)) logoFailures.push(`${l.file}: in EDITOR_LOGOS but not in src/assets/editor-logos`);
}
if (logoFailures.length > 0) {
  console.error("Editor logo check failed (web/scripts/third-party-licenses.mjs):");
  for (const f of logoFailures) console.error(`  ${f}`);
  process.exit(1);
}
out += `\n${rule}\nGo standard library ${goToolchainVersion()}\nLicence: BSD-3-Clause\n`;
out += "The Graphvein binary links the Go standard library; the Go route\n" +
  "extractor also transcribes net/http's ServeMux pattern code\n" +
  "(internal/extract/routes/gohttp_mux.go).\n\n";
out += GO_NOTICE;

out += `\n${rule}\nEditor logos\n\n`;
out +=
  "The editor picker and the \"Open in\" links show the official logos of\n" +
  "four editors under their brands' conditions; the other editors are shown\n" +
  "as neutral monograms. Bundling in a distributed product has not been\n" +
  "confirmed with Microsoft or JetBrains (see docs/adr/0050).\n\n";
out += EDITOR_LOGOS.flatMap((l) => (l.text === null ? [] : [l.text])).join("\n");

for (const key of Object.keys(NOTICES)) {
  if (!packages.some((p) => `${p.name}@${p.version}` === key)) {
    console.error(`${key}: a notice is written for it, but it is not a runtime package in package-lock.json`);
    process.exit(1);
  }
}

writeFileSync(outFile, out);
console.log(
  `Wrote ${String(packages.length - BUNDLED_TOOLS.length)} runtime package and ${String(BUNDLED_TOOLS.length)} build tool notices to internal/server/ui/static/third-party-licenses.txt`,
);
