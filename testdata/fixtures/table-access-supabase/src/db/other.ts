import { supabase, tenant } from "../lib/client";
import { createClient } from "@supabase/supabase-js";

// D: an rpc call runs a function; no table is claimed.
export async function search(q: string) {
  return supabase.rpc("search_products", { q });
}

// D: a storage bucket is no table.
export async function upload(file: Blob) {
  return supabase.storage.from("avatars").upload("a.png", file);
}

// A builder returned without a verb (T024): an unknown
// builder_unread, its uses are not read.
export function builder() {
  const query = supabase.from("profiles");
  return query;
}

// SB-T4: a non-literal table name.
export async function remove(table: string, id: string) {
  return supabase.from(table).delete().eq("id", id);
}

// SB-T5: receivers SB-K1 does not prove.
export async function parameter(client: ReturnType<typeof createClient>) {
  return client.from("profiles").select("*");
}

export async function mutable() {
  let sb = createClient("u", "k");
  return sb.from("profiles").select("*");
}

export async function destructured(make: () => { client: any }) {
  const { client } = make();
  return client.from("profiles").select("*");
}

export class Repo {
  constructor(private db: any) {}
  list() {
    return this.db.from("profiles").select("*");
  }
}

export async function tenantProfiles() {
  return tenant.from("profiles").select("*");
}
