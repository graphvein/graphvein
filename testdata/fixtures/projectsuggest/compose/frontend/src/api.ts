// Calls of the frontend (the web service): through API_URL and JOBS_URL,
// and one with the literal origin of BROWSER_API. Every URL uses /b6b so
// the repository's own self-index links no other fixture.
export function users() {
  return fetch(`${process.env.API_URL}/b6b/compose-users`);
}
export function jobs() {
  return fetch(`${process.env.JOBS_URL}/b6b/compose-jobs`);
}
export function browser() {
  return fetch('http://localhost:18080/b6b/compose-browser');
}
