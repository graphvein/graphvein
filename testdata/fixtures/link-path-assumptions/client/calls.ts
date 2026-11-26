export async function calls() {
  await fetch("/z/b");
  await fetch("/y/c");
  await fetch("/w/c");
  await fetch("/plain/one");
  await fetch("/health/live");
  await fetch("/gw/items/all");
  await fetch("/v1/gp/orders/list");
  await fetch("/gb/items/all");
  await fetch("/direct/gb/items/all");
}
