import { NextResponse } from 'next/server';

export async function GET(request: Request) {
  if (request.headers.get('x-fail')) {
    return NextResponse.json({ error: 'fail' }, { status: 500 });
  }
  return NextResponse.json({ ok: true });
}
