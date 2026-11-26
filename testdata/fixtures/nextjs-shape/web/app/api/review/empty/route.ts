import { NextResponse } from 'next/server';

// Response.json of undefined throws: Next.js answers 500 (row SH-N22).
export async function GET() {
  return Response.json(undefined);
}

export async function POST() {
  // @ts-expect-error the value argument is required
  return NextResponse.json();
}
