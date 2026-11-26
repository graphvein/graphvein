// Checks the licence of every package in package-lock.json against the
// allow-lists of ADR 0041. It reads only the lockfile, so it needs no
// dependency and runs before or without node_modules.
//
// Runtime packages (shipped in the bundle) may use only the permissive
// licences in RUNTIME. So may the build tools in BUNDLED_TOOLS, whose own
// code the build copies into the bundle. Other development tools may
// additionally use DEV_EXTRA.
// A missing licence or an SPDX expression (OR/AND) fails: those need a
// human decision, recorded by extending the lists through the ADR.
//
// EXCEPTIONS holds the only such decisions: one package name, one exact
// version and one exact licence string each. ADR 0046 accepts elkjs 0.12.0
// under "EPL-2.0 OR GPL-3.0-or-later" and records that Graphvein takes it
// under EPL-2.0. Any other version of elkjs fails, so a version change is
// reviewed again instead of inheriting the exception.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { BUNDLED_TOOLS } from "./bundled-tools.mjs";

const RUNTIME = ["MIT", "ISC", "BSD-2-Clause", "BSD-3-Clause", "Apache-2.0", "0BSD"];
const DEV_EXTRA = ["MPL-2.0", "CC-BY-4.0", "CC0-1.0", "MIT-0", "BlueOak-1.0.0"];
const EXCEPTIONS = [{ name: "elkjs", version: "0.12.0", licence: "EPL-2.0 OR GPL-3.0-or-later", chosen: "EPL-2.0", adr: "0046" }];

const lockPath = fileURLToPath(new URL("../package-lock.json", import.meta.url));
const lock = JSON.parse(readFileSync(lockPath, "utf8"));

const failures = [];
const counts = new Map();
for (const [key, entry] of Object.entries(lock.packages ?? {})) {
  if (key === "") continue; // the project itself
  const name = key.slice(key.lastIndexOf("node_modules/") + "node_modules/".length);
  // Only packages marked dev are build tools. devOptional packages (for
  // example @types/react) are held to the runtime list too: stricter and
  // harmless, since they are all MIT today.
  const bundled = key === `node_modules/${name}` && BUNDLED_TOOLS.includes(name);
  const dev = entry.dev === true && !bundled;
  const allowed = dev ? RUNTIME.concat(DEV_EXTRA) : RUNTIME;
  const licence = entry.license;
  const kind = bundled ? "bundled tool" : dev ? "dev" : "runtime";
  if (typeof licence !== "string" || licence === "") {
    failures.push(`${name}@${entry.version}: no license field in package-lock.json`);
    continue;
  }
  const exception = EXCEPTIONS.find((x) => x.name === name);
  if (exception !== undefined) {
    if (entry.version !== exception.version || licence !== exception.licence || dev) {
      failures.push(
        `${name}@${entry.version}: ${kind} licence ${licence} does not match the ADR ${exception.adr} exception ` +
          `(${exception.name}@${exception.version}, runtime, ${exception.licence}); review the new version`,
      );
      continue;
    }
    const label = `runtime ${licence} (exception, taken as ${exception.chosen}, ADR ${exception.adr})`;
    counts.set(label, (counts.get(label) ?? 0) + 1);
    continue;
  }
  if (!allowed.includes(licence)) {
    failures.push(`${name}@${entry.version}: ${kind} licence ${licence} is not allowed`);
    continue;
  }
  const label = `${kind} ${licence}`;
  counts.set(label, (counts.get(label) ?? 0) + 1);
}

for (const x of EXCEPTIONS) {
  if (lock.packages?.[`node_modules/${x.name}`] === undefined) {
    failures.push(`${x.name}: has a licence exception but is not in package-lock.json; remove the exception`);
  }
}

for (const name of BUNDLED_TOOLS) {
  if (lock.packages?.[`node_modules/${name}`] === undefined) {
    failures.push(`${name}: listed in BUNDLED_TOOLS but not in package-lock.json`);
  }
}

for (const label of [...counts.keys()].sort()) {
  console.log(`${label}: ${counts.get(label)}`);
}
if (failures.length > 0) {
  console.error("Licence check failed:");
  for (const f of failures) console.error(`  ${f}`);
  process.exit(1);
}
console.log("Licence check passed.");
