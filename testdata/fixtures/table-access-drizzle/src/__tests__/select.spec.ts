import { db } from "../db/client";
import { users } from "../db/schema";

export async function seedUser() {
  await db.insert(users).values({ email: "a@example.com" });
}
