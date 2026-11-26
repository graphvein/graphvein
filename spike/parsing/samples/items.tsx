import React from "react";
import { loadItems } from "./client";

export function Items() {
  fetch("/api/items");
  return <section>Items</section>;
}

export const ItemCard = () => <article>Item</article>;

export class ItemStore {
  items: string[] = [];
}
