import { supabase, browser, billing, PRODUCTS } from "../lib/client";
import { createServer, createServerTwice, routeClient } from "@/lib/client";
import { createClient } from "@supabase/supabase-js";

export async function profiles() {
  return await supabase.from("profiles").select("*").eq("id", "x").single();
}

export async function productsByConst() {
  return supabase.from(PRODUCTS).select("id, name");
}

export async function direct() {
  return createClient("u", "k").from("tags").select("*");
}

export async function local() {
  const sb = createClient("u", "k");
  return sb.from("prices").select("amount", { count: "exact", head: true });
}

export async function server() {
  const sb = await createServer();
  return sb.from("subscriptions").select("id");
}

export async function serverTwice() {
  const sb = await createServerTwice();
  return sb.from("subscriptions").select("id");
}

export async function route(cookies: unknown) {
  const sb = routeClient(cookies);
  return sb.from("messages").select("body");
}

export async function invoices() {
  return billing.from("invoices").select("*");
}

export async function invoicesBySchema() {
  return browser.schema("billing").from("invoices").select("id");
}

export async function invoicesByDynamicSchema(name: string) {
  return browser.schema(name).from("invoices").select("id");
}

export async function view() {
  return browser.from("active_products").select("*");
}

export async function missing() {
  return browser.from("missing_table").select("*");
}
