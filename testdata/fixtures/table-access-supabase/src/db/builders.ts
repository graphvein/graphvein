import { supabase } from "../lib/client";

// A query builder bound to a const and used with a verb in the same
// function (T024): read as one chain, a read of products.
export async function stored() {
  const q = supabase.from("products");
  return q.select("id");
}

// A builder returned to the caller: its uses are not read, so an unknown
// (builder_unread), never silence.
export function builderOnly() {
  return supabase.from("prices");
}

// A let binding may hold another builder at the use: not followed; both
// builders are unknowns.
export async function letBuilder() {
  let q = supabase.from("prices");
  q = supabase.from("products");
  return q.select();
}
