const text = document.querySelector("script#config__json")?.textContent || "{}";
const pageConfig = JSON.parse(text);
const config = { maxLogs: 400, ...pageConfig };
export default Object.freeze(config);

// UP-39: the proven helper shapes.
export const withBase = (path: string) => `${config.base}${path}`;
export const plusBase = (path: string) => config.base + path;
export function fnBase(path: string) {
  return `${config.base}${path}`;
}
const BASE = pageConfig.base;
export const rootBase = (path: string) => BASE + path;

// UP-40: a body of the shape whose base is not proven.
let mutableBase = config.base;
export const letBase = (path: string) => `${mutableBase}${path}`;
export const callBase = (path: string) => `${pageConfig.read()}${path}`;
const written = { ...pageConfig };
written.base = "/z";
export const writtenBase = (path: string) => `${written.base}${path}`;
const KNOWN = { base: "/k" };
export const knownBase = (path: string) => `${KNOWN.base}${path}`;
const EMPTY = { api: "/k" };
export const missingBase = (path: string) => `${EMPTY.base}${path}`;

// D: other helper shapes keep the ordinary evaluation.
export const twoParams = (path: string, query: string) => `${config.base}${path}${query}`;
export const suffixed = (path: string) => `${config.base}${path}/`;
export const prefixed = (path: string) => `/x${config.base}${path}`;
