import { NextResponse, type NextRequest } from 'next/server';
import { cookies } from 'next/headers';

// Route segment config (row SH-N24): with force-static every handler of
// the file sees an empty query, empty headers and cookies.
export const dynamic = 'force-static';

export async function GET(request: NextRequest) {
  const q = request.nextUrl.searchParams.get('q');
  const h = request.headers.get('x-a');
  if (!q) {
    return NextResponse.json({ error: 'q', h }, { status: 400 });
  }
  return NextResponse.json({ q, h });
}

export async function POST(request: NextRequest) {
  const token = (await cookies()).get('token');
  const body = await request.json();
  if (!token) {
    return NextResponse.json({ error: 'token' }, { status: 401 });
  }
  return NextResponse.json({ name: body.name }, { status: 201 });
}
