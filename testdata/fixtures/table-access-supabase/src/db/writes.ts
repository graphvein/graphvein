import { supabase } from "../lib/client";

export async function addProfile(row: object) {
  return supabase.from("profiles").insert(row);
}

export async function addProfileReturning(row: object) {
  return supabase.from("profiles").insert(row).select("*").single();
}

export async function renameProduct(id: string, name: string) {
  return supabase.from("products").update({ name }).eq("id", id);
}

export async function savePrice(row: object) {
  return supabase.from("prices").upsert(row, { onConflict: "id" });
}

export async function dropTag(id: number) {
  return supabase.from("tags").delete().eq("id", id);
}

export async function addPriceWithProduct(row: object) {
  return supabase.from("prices").insert(row).select("id, products(name)");
}
