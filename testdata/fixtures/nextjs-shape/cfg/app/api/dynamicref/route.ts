import { NextResponse, type NextRequest } from 'next/server';

// A dynamic value that is not a string literal may be 'force-static'
// (Next.js 14.1.0 applies it; 15 and 16 refuse to build it).
const mode = 'force-static';
export const dynamic = mode;

export async function GET(request: NextRequest) {
  const q = request.nextUrl.searchParams.get('q');
  return NextResponse.json({ q }, { status: q ? 200 : 400 });
}
