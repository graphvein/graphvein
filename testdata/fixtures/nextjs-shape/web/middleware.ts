import { NextResponse, type NextRequest } from 'next/server';

// Runs only for /admin paths: the matcher is a literal (row SH-N24).
export function middleware(request: NextRequest) {
  if (!request.cookies.get('admin')) {
    return NextResponse.redirect(new URL('/', request.url));
  }
  const res = NextResponse.next();
  res.headers.set('x-admin', '1');
  return res;
}

export const config = { matcher: '/admin/:path*' };
