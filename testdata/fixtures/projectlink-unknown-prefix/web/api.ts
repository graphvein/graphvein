import { withBase } from "./config";

export async function calls(id: string) {
  fetch(withBase("/upd/orders/list"));
  fetch(`/v1/upd/order/${id}`);
  fetch("https://api.example.com/upd/orders/list");
  fetch(withBase("/upd/shared/items"));
  fetch(withBase("/upd/tie/items"));
  fetch(withBase("/updping"));
  fetch(`${import.meta.env.VITE_API}/upd/order/${id}`);
}
