import { createClient } from "@supabase/supabase-js";

const supabase = createClient("u", "k");

// No Supabase set in this package: its generated types are the schema
// source (T024c3, SB-G1; assumes SB-V6).
export async function boards() {
  return supabase.from("boards").select("*");
}

export async function addCard(text: string) {
  return supabase.from("cards").insert({ board_id: 1, text });
}

// Embeds never resolve through generated types: partial.
export async function boardCards() {
  return supabase.from("boards").select("id, cards(*)");
}

// A name the generated types do not hold: sql_name_unresolved.
export async function archived() {
  return supabase.from("archived_boards").select("*");
}
