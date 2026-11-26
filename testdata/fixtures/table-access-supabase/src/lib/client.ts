import { createClient } from "@supabase/supabase-js";
import { createBrowserClient, createServerClient } from "@supabase/ssr";
import { createRouteHandlerClient } from "@supabase/auth-helpers-nextjs";

const url = process.env.NEXT_PUBLIC_SUPABASE_URL!;
const key = process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY!;

// SB-K1: a module const bound to a factory call.
export const supabase = createClient(url, key);

// SB-K1: db.schema from a literal.
export const billing = createClient(url, key, { db: { schema: "billing" } });

// SB-K1: db.schema that is no literal: every site of this client is SB-T5.
export const tenant = createClient(url, key, { db: { schema: process.env.SCHEMA } });

export const browser = createBrowserClient(url, key);

// SB-K1: one level of a function whose every return is a proven client.
export async function createServer() {
  const cookieStore = { get: (name: string) => name };
  return createServerClient(url, key, { cookies: cookieStore });
}

export const routeClient = (cookies: unknown) => {
  return createRouteHandlerClient({ cookies });
};

// Two levels: a wrapper of a wrapper is not followed.
export async function createServerTwice() {
  return await createServer();
}

export const PRODUCTS = "products";
