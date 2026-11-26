import { NextResponse } from 'next/server';

function withAuth(h: (req: Request) => Promise<Response>) {
  return async (req: Request) => h(req);
}

export const GET = withAuth(async (req) => NextResponse.json({ user: req.headers.get('x-user') }));
