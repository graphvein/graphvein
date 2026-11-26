import { NextResponse } from 'next/server';
import { notFound, redirect } from 'next/navigation';

// next/navigation calls in nested functions do not run with the handler
// (row SH-N23).
export async function GET() {
  const unused = () => notFound();
  function later() {
    redirect('/y');
  }
  [1].forEach(() => later);
  return NextResponse.json({ ok: 1, unused: typeof unused });
}
