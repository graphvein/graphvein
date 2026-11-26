import { NextResponse, type NextRequest } from 'next/server';

// dynamic = 'error' (row SH-N24): Next.js 14.1.0 throws on every read of
// the request (500); 16.2.10 passes it as sent.
export const dynamic = 'error';

export async function POST(request: NextRequest) {
  const body = await request.json();
  const h = request.headers.get('x-a');
  return NextResponse.json({ name: body.name, h }, { status: 201 });
}
