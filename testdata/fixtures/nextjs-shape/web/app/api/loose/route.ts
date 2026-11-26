import { NextResponse } from 'next/server';

export async function POST(request: Request) {
  const raw = await request.json();
  const copy = { ...raw };
  const keys = Object.fromEntries(new URL(request.url).searchParams);
  request.headers.forEach(() => undefined);
  if (raw.kind === 'a') {
    return NextResponse.json(copy, { status: 204 });
  }
  const res = NextResponse.json({ kind: raw.kind });
  res.headers.set('content-type', 'text/plain');
  return res;
}
