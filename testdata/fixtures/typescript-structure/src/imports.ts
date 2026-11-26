/// <reference path="../types/globals.d.ts" />
/// <reference types="node" />
import "./side-effect";
import type { T } from "./lib/types";
import { type U, value } from "./lib/mixed";
import data from "./data.json";
import logo from "./assets/logo.svg";
import * as ns from "@/lib/mixed";
import { x as y } from "multi/only";
import { a } from "@probe/a";
import { sub } from "@probe/a/sub";
import { hidden } from "@probe/a/src/hidden";
import { deep } from "@probe/a/internal/deep";
import { b } from "@probe/b";
import { c } from "@probe/c";
import _ from "lodash";
import { d } from "@probe/d";
import { e } from "@probe/e";
import { t } from "@probe/t";
import { dup } from "@probe/dup";
import icon from "~icons/mdi/home";
import pwa from "virtual:pwa-register";
import { gone } from "@/missing/gone";
import "/public/logo.png";
import { nope } from "./nope";
import type { A } from "./lib/types" with { "resolution-mode": "import" };
import fs from "node:fs";
import path = require("path");
import local = require("./lib/cjs-target");
import { dts } from "./lib/both";
import { idx } from "./lib/dir";
import { js } from "./lib/plain.js";
import { mjs } from "./lib/mod.mjs";
import { cjsx } from "./lib/cm.cjs";
import { upper } from "./lib/upper";
export * from "./lib/mixed";
export * as space from "./lib/mixed";
export { value as renamed } from "./lib/mixed";
export type { T as TT } from "./lib/types";
const lazy = import("./lib/lazy");
const dyn = import(`./lib/${y}`);
const req = require("./lib/required");
const req2 = require(y);
declare module "virtual-mod" {
  import { a } from "./lib/mixed";
}
export { d, e, t, dup, icon, pwa, gone, nope };
export { ns, data, logo, a, sub, hidden, deep, b, c, _, fs, path, local, dts, idx, js, mjs, cjsx, upper, lazy, dyn, req, req2, T, U };
