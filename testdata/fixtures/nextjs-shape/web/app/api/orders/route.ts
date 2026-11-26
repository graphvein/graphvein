import { NextRequest, NextResponse } from 'next/server';
import { z } from 'zod';
import { badRequest, created, reply } from '../../../lib/responses';
import { applyPatch, audit, saveOrder, type OrderPatch } from '../../../lib/orders';

const Order = z.object({
  sku: z.string(),
  qty: z.number().int(),
  note: z.string().optional(),
});

export async function POST(request: NextRequest) {
  const json = await request.json();
  const parsed = Order.safeParse(json);
  if (!parsed.success) {
    return badRequest('invalid order');
  }
  await audit(request);
  const saved = await saveOrder({ ...json, at: Date.now() });
  return created(saved);
}

export async function PATCH(request: NextRequest) {
  const input = (await request.json()) as OrderPatch;
  applyPatch(input);
  const status = input.qty > 0 ? 200 : 202;
  return NextResponse.json({ ok: true }, { status });
}

export async function DELETE() {
  throw new Error('not allowed');
}

export async function PUT(request: NextRequest) {
  const raw = await request.text();
  return reply(raw, 200);
}
