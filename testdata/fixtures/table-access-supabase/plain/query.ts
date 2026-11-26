// Another library's from(...).select() in a package that declares no
// Supabase package: invisible (no SB-T5 record).
export function users(knex: any) {
  return knex.from("users").select("*");
}
