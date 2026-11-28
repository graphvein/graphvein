export async function getAbout() {
  return fetch("/api/about");
}

export async function createToken(body: string) {
  return fetch("/api/token/", { method: "POST", body });
}

export async function getDebug() {
  return fetch("/api/debug");
}

export async function getStats() {
  return fetch("/stats");
}

export async function getAdminStats() {
  return fetch("/admin/stats");
}
