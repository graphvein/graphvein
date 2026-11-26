import { NextResponse } from 'next/server';
import { gone, relay } from '../../../../lib/nav';

export async function GET() {
  try {
    return await gone();
  } catch (e) {
    return NextResponse.json({ caught: true }, { status: 500 });
  }
}

export async function POST() {
  return gone();
}

export async function PUT(request: Request) {
  return relay(request);
}

export async function PATCH() {
  return gone().catch(() => Response.json({ caught: true }, { status: 503 }));
}
