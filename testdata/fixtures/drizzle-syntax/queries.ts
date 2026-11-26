// GD: imports only the root drizzle-orm module (queries), so no payload.
import { eq, sql } from "drizzle-orm";
import { task } from "./schema";

export const byId = (id: number) => eq(task.id, id);
export const raw = sql`select 1`;
