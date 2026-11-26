export async function auto(id: string) {
  fetch(withBase("/upc/api/version"));
  fetch(withBase(`/upc/hosts/${id}/logs`));
  fetch(letBase("/upc/api/version"));
}
