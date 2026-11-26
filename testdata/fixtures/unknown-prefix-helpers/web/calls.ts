import config, { withBase, plusBase, fnBase, rootBase, letBase, callBase, writtenBase, knownBase, missingBase, twoParams, suffixed, prefixed } from "./config";

export async function calls(id: string, source: string) {
  fetch(withBase("/upc/api/version"));
  fetch(withBase("/upc/api/profile"), { method: "PATCH" });
  fetch(plusBase(`/upc/hosts/${id}/logs`));
  fetch(fnBase("/upc/api/version"));
  fetch(rootBase("/upc/api/version"));
  fetch(withBase("/upchealth"));
  fetch(withBase("/upc/shared/list"));
  fetch(withBase("/upc/inner/x"));
  fetch(withBase("/upc/known/items"));
  fetch(withBase(source));
  fetch(withBase("upc/api/version"));
  fetch(withBase(`${source}?window=1`));
  fetch(letBase("/upc/api/version"));
  fetch(callBase("/upc/api/version"));
  fetch(writtenBase("/upc/api/version"));
  fetch(knownBase("/upc/known/items"));
  fetch(missingBase("/upc/api/version"));
  fetch(twoParams("/upc/api/version", ""));
  fetch(suffixed("/upc/api/version"));
  fetch(prefixed("/upc/api/version"));
  const local = (path: string) => `${config.base}${path}`;
  fetch(local("/upc/api/version"));
}
