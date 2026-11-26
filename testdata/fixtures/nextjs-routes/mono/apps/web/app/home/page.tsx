'use client';

// Row NX-L1 (T024): a relative URL resolves against the web
// application's origin; /api/stats is a route of the admin application
// only, so the call is unmatched.
export default function Home() {
  fetch('/api/stats');
  return null;
}
