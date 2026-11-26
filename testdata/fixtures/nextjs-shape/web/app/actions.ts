'use server';

import { cookies } from 'next/headers';
import { redirect } from 'next/navigation';
import { z } from 'zod';

export interface RenameInput {
  id: string;
  name: string;
}

export interface RenameResult {
  id: string;
  name: string;
  renamed: boolean;
}

const Signup = z.object({ email: z.string(), age: z.number() });

export async function subscribe(formData: FormData) {
  const email = formData.get('email');
  const topics = formData.getAll('topic');
  const jar = await cookies();
  jar.get('ref');
  if (!email) {
    return { error: 'email required' };
  }
  return { ok: true, topics: topics.length };
}

export async function rename(input: RenameInput): Promise<RenameResult> {
  return { id: input.id, name: input.name, renamed: true };
}

export async function signup(data: unknown) {
  const value = Signup.parse(data);
  redirect('/welcome?email=' + value.email);
}

export async function move(from: string, to: number) {
  return from.length + to;
}

function withUser<T>(fn: T): T {
  return fn;
}

export const wrapped = withUser(async () => ({ ok: true }));
