import { NextResponse } from 'next/server';

export async function GET(request: Request) {
  const scope = request.headers.get('x-scope');
  if (!scope) {
    return NextResponse.json({ error: 'scope' }, { status: 403 });
  }
  return NextResponse.json({ visits: 3 });
}
