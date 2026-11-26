import { NextResponse, type NextRequest } from 'next/server';

// revalidate and force-dynamic leave the request as sent (row D): reading
// it makes the route dynamic.
export const revalidate = 60;
export const runtime = 'nodejs';

export async function GET(request: NextRequest) {
  const q = request.nextUrl.searchParams.get('q');
  if (!q) {
    return NextResponse.json({ error: 'q' }, { status: 400 });
  }
  return NextResponse.json({ q });
}
