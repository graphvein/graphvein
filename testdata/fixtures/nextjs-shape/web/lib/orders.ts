import type { NextRequest } from 'next/server';

export interface OrderPatch {
  qty: number;
  note?: string | null;
}

export async function audit(request: NextRequest) {
  const id = request.headers.get('x-request-id');
  return id;
}

export function applyPatch(patch: OrderPatch) {
  return patch.qty;
}

export async function saveOrder(order: { sku: string }) {
  return order;
}
