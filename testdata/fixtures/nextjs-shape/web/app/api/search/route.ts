import { NextResponse } from 'next/server';
import { cookies, headers } from 'next/headers';

export async function GET(request: Request) {
  const { searchParams } = new URL(request.url);
  const term = searchParams.get('term');
  const pages = searchParams.getAll('page');
  const h = await headers();
  const lang = h.get('Accept-Language');
  const session = (await cookies()).get('session');
  if (!term) {
    return new Response('missing term', { status: 400 });
  }
  if (term === 'go') {
    return NextResponse.redirect(new URL('/api/plain', request.url));
  }
  if (term === 'moved') {
    return Response.redirect('https://example.com/', 301);
  }
  if (term === 'none') {
    return new Response(null, { status: 204 });
  }
  if (term === 'next') {
    return NextResponse.next();
  }
  return Response.json({ results: [], term }, { headers: { 'Cache-Control': 'no-store' } });
}
