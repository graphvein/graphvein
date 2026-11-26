import { NextRequest, NextResponse } from 'next/server';
import { notFound, redirect } from 'next/navigation';
import { z } from 'zod';
import { findItem } from '../../../../lib/items';

const ItemBody = z.object({
  name: z.string().min(1),
  price: z.number(),
  tags: z.array(z.string()).optional(),
});

export const dynamic = 'force-dynamic';

export async function GET(request: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const q = request.nextUrl.searchParams.get('q');
  const verbose = request.nextUrl.searchParams.has('verbose');
  const auth = request.headers.get('Authorization');
  const theme = request.cookies.get('theme')?.value;
  if (id === 'missing') {
    notFound();
  }
  if (id === 'old') {
    redirect('/api/items/new');
  }
  const item = await findItem(id);
  if (!item) {
    return NextResponse.json({ error: 'not found' }, { status: 404 });
  }
  return NextResponse.json(item);
}

export async function PUT(request: NextRequest) {
  const body = ItemBody.parse(await request.json());
  return NextResponse.json({ ok: true, name: body.name }, { status: 200 });
}
