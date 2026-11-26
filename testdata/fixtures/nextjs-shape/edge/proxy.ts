import { NextResponse } from 'next/server';

// A Next.js 16 proxy with an array matcher: it may run before every route
// (row SH-N24; array matchers are not read).
export function proxy() {
  return NextResponse.next();
}

export const config = { matcher: ['/api/:path*', '/other'] };
