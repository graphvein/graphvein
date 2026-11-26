import { NextResponse } from 'next/server';
import { headers } from 'next/headers';

// headers() without await: Next.js 16 throws (row SH-N6).
export async function GET() {
  const h = (headers() as any).get('x-a');
  return NextResponse.json({ h }, { status: 201 });
}
