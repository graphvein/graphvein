import { createClient } from "@supabase/supabase-js";

const supabase = createClient("u", "k");

// No Supabase set in this package: no_schema_source.
export async function notes() {
  return supabase.from("notes").select("*");
}
