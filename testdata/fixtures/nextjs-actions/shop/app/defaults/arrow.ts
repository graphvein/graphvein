'use server';

// Row SA-M1, default_async_arrow (T024): Next.js registers an
// anonymous async arrow as the default action.
export default async (data: FormData) => data.get('note');
