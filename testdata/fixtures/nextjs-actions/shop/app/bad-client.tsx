'use client';

// Row D: an inline 'use server' function in a 'use client' file.
export function Bad() {
  async function nope() {
    'use server';
  }
  return <form action={nope} />;
}
