import { eq, sql } from "drizzle-orm";
import { db } from "./db/client";
import { auditLog, comments, posts, tags, users } from "./db/schema";

// CT-Z2: insert, update and delete builders.
export async function createUser(email: string) {
  return db.insert(users).values({ email }).returning();
}

export async function upsertTag(label: string) {
  return db.insert(tags).values({ label }).onConflictDoUpdate({ target: tags.label, set: { label } });
}

export async function ensureTag(label: string) {
  return db.insert(tags).values({ label }).onConflictDoNothing();
}

export async function rename(id: number, name: string) {
  return db.update(users).set({ name }).where(eq(users.id, id));
}

// update ... from reads its from table.
export async function retitleFromAuthor() {
  return db
    .update(posts)
    .set({ title: sql`${users.name}` })
    .from(users)
    .where(eq(posts.authorId, users.id));
}

// A join on an update chain is no listed form: site_partial.
export async function updateWithJoin() {
  return db
    .update(posts)
    .set({ title: "x" })
    .from(users)
    .leftJoin(comments, eq(comments.postId, users.id))
    .where(eq(posts.authorId, users.id));
}

export async function removeComment(id: number) {
  return db.delete(comments).where(eq(comments.id, id));
}

// insert ... select: the inner select is its own CT-Z1 site.
export async function copyTags() {
  return db.insert(tags).select(db.select({ id: tags.id, label: tags.label }).from(tags));
}

// A transaction callback's sites belong to the enclosing function.
export async function logAction(action: string, userId: number) {
  await db.transaction(async (tx) => {
    await tx.insert(auditLog).values({ action, userId });
    await tx.update(users).set({ name: null }).where(eq(users.id, userId));
  });
}

// Row D: a transaction whose callback holds no site.
export async function inTransaction(work: (tx: unknown) => Promise<void>) {
  await db.transaction(async (tx) => {
    await work(tx);
  });
}

// A target that is a parameter: unknown, no claim.
export async function wipe(table: typeof users) {
  return db.delete(table).where(sql`true`);
}

// Not sites: a literal argument, a Map without a builder chain.
export function notSites(params: URLSearchParams, cache: Map<string, number>, key: string) {
  params.delete("page");
  cache.delete(key);
}
