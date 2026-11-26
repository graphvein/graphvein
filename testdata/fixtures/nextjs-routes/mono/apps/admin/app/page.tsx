'use client';

// Linked: the admin application's own route.
export default function AdminHome() {
  fetch('/api/stats');
  return null;
}
