import { supabase } from "../lib/client";

export async function productPrices() {
  return supabase.from("products").select("*, prices(*)");
}

export async function subscriptionPlans() {
  return supabase.from("subscriptions").select("*, prices(*, products(*))");
}

export async function priceProduct() {
  return supabase.from("prices").select(`
    amount,
    item:products ( name )
  `);
}

export async function hinted() {
  return supabase.from("profiles").select("id, sent:messages!sender_id(*)");
}

export async function several() {
  return supabase.from("profiles").select("id, messages(*)");
}

export async function junction() {
  return supabase.from("products").select("id, tags(*)");
}

export async function spread() {
  return supabase.from("products").select("id, ...prices(amount)");
}

export async function embeddedView() {
  return supabase.from("products").select("id, active_products(*)");
}

export async function aggregate() {
  return supabase.from("prices").select("count(), amount.sum()");
}

// A select argument that is not a literal (T024c3): the base table and a
// partial unknown, embedded tables not read.
const columns = process.env.PRODUCT_COLUMNS ?? "*";

export async function dynamicColumns() {
  return supabase.from("products").select(columns);
}

// A final join modifier is PostgREST's join type (T024): the
// embed resolves as without it; a hint before it still refuses.
export async function innerJoin() {
  return supabase.from("subscriptions").select("*, prices!inner(*, products!left(*))");
}

export async function hintedInner() {
  return supabase.from("profiles").select("id, messages!sender_id!inner(*)");
}
