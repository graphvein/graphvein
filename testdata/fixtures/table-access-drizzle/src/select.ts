import { eq, inArray, isNotNull, sql } from "drizzle-orm";
import { alias } from "drizzle-orm/pg-core";
import { members, tablesByName } from "./db/aliases";
import { db } from "./db/client";
import * as schema from "./db/schema";
import { activeUsers, comments, posts, users } from "./db/tables";

// CT-Z1: select builders. Table consts arrive through the export * barrel,
// a namespace import, a module-level alias const, a local const and
// alias(t, "a").
export async function listUsers() {
  return db.select().from(users);
}

export async function distinctNames() {
  return db.selectDistinct({ name: members.name }).from(members);
}

export async function latestPerAuthor() {
  return db
    .selectDistinctOn([posts.authorId])
    .from(posts)
    .orderBy(posts.authorId, posts.publishedAt);
}

export async function postsWithAuthors() {
  return db
    .select()
    .from(posts)
    .leftJoin(users, eq(posts.authorId, users.id))
    .innerJoin(comments, eq(comments.postId, posts.id));
}

export async function rightAndFull() {
  return db
    .select()
    .from(schema.posts)
    .rightJoin(schema.users, eq(schema.posts.authorId, schema.users.id))
    .fullJoin(schema.comments, eq(schema.comments.postId, schema.posts.id));
}

export async function fromObject() {
  return db.select().from(tablesByName.posts);
}

export async function crossTags() {
  const t = schema.tags;
  return db.select().from(users).crossJoin(t);
}

export async function managers() {
  const manager = alias(users, "manager");
  return db.select().from(users).leftJoin(manager, eq(users.managerId, manager.id));
}

export async function countUsers() {
  return db.$count(users);
}

// A subquery bound to a local builds; the lateral join's subquery is no
// table const: the site claims users and records a partial unknown.
export async function lateral() {
  const recent = db.select().from(posts).limit(1).as("recent");
  return db.select().from(users).leftJoinLateral(recent, sql`true`);
}

// The subquery in inArray is its own site, executed by the outer one.
export async function authorsWithPosts() {
  return db
    .select()
    .from(users)
    .where(inArray(users.id, db.select({ id: posts.authorId }).from(posts)));
}

// Refusals: a $with CTE, a view, a parameter of a generic helper.
export async function withCte() {
  const named = db.$with("named").as(db.select().from(users).where(isNotNull(users.name)));
  return db.with(named).select().from(named);
}

export async function fromView() {
  return db.select().from(activeUsers);
}

export async function firstRow(table: typeof users) {
  return db.select().from(table).limit(1);
}

// A module-level site: the owner is the variable node.
export const allPosts = db.select().from(posts);

// Not sites: another library's string table, a Set.
export function notSites(knex: { select(): { from(t: string): unknown } }, seen: Set<number>) {
  knex.select().from("users");
  seen.delete(1);
}
