import { eq } from "drizzle-orm";
import { db } from "./db/client";
import { users } from "./db/schema";

// CT-Z3: relational queries; the key is an export name of the schema
// module, with relations from orm_relations.
export async function userWithPosts(id: number) {
  return db.query.users.findFirst({
    where: eq(users.id, id),
    with: { posts: { with: { comments: true } }, manager: true },
  });
}

export async function postsWithTags() {
  return db.query.posts.findMany({ with: { postTags: { with: { tag: true } }, author: false } });
}

export async function partialQueries(extra: object) {
  await db.query.users.findMany({ ...extra, with: { posts: true } });
  await db.query.users.findMany({ with: { profile: true } });
  await db.query.users.findMany({ with: { followers: true } });
  await db.query.comments.findMany(extra);
}

// A key that names no table const: unknown.
export async function notATable() {
  return db.query.audit.findMany();
}
