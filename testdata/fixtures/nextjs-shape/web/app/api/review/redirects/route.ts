import { NextResponse, type NextRequest } from 'next/server';

// Redirect targets (row SH-N19): only a URL proven absolute is claimed; a
// relative URL makes both helpers throw, so Next.js answers 500.
export async function GET() {
  return NextResponse.redirect('/login');
}

export async function POST() {
  return Response.redirect('/login', 301);
}

export async function PUT(request: NextRequest) {
  const url = request.nextUrl.clone();
  url.pathname = '/api/plain';
  return NextResponse.redirect(url);
}

export async function PATCH() {
  return NextResponse.redirect('https://example.com/a', 308);
}

export async function DELETE(request: NextRequest) {
  return Response.redirect(new URL('/api/plain', request.url), 303);
}
