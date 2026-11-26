import Joi from 'joi';
import { NextResponse } from 'next/server';
import { z } from 'zod';

const Event = z.object({ type: z.string() });
const Ping = Joi.object({ id: Joi.string().required() });

export async function POST(request: Request) {
  let event;
  try {
    event = Event.parse(await request.json());
  } catch {
    return NextResponse.json({ error: 'bad event' }, { status: 422 });
  }
  return NextResponse.json({ type: event.type });
}

export async function PUT(request: Request) {
  const body = await request.json();
  const { error } = Ping.validate(body);
  if (error) {
    return NextResponse.json({ error: error.message }, { status: 400 });
  }
  return new Response(null, { status: 204 });
}
