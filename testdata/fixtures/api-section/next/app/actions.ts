'use server';

// Server actions (T024b): one used by a form hook and a direct call, one
// never used.
export async function t016dSave(data: FormData) {
  return data.get('name');
}

export async function t016dUnused() {
  return 1;
}

// Passed as a prop only: an unread use may reach it (flag unread_use).
export async function t016dPassed() {
  return 2;
}
