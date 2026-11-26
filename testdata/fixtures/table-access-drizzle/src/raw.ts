import { sql } from "drizzle-orm";
import { db } from "./db/client";
import { auditLog, posts, users } from "./db/schema";

// CT-Z4: sql templates run by execute. A table const renders as its name,
// a column as "table"."column", any other value is a bound parameter.
export async function countPosts(authorId: number) {
  return db.execute(sql`select count(*) from ${posts} where ${posts.authorId} = ${authorId}`);
}

export async function rawNames() {
  return db.execute(sql`SELECT u.name FROM users u JOIN posts p ON p.author_id = u.id`);
}

export async function touchAudit(action: string) {
  return db.execute(sql`insert into ${auditLog} (action) values (${action})`);
}

export async function bump() {
  await db.execute(sql`UPDATE posts SET title = title || '!' WHERE id IN (SELECT post_id FROM comments)`);
}

// No table: claims nothing, records nothing.
export async function lock() {
  await db.execute(sql`SELECT pg_advisory_xact_lock(1)`);
}

// sql_dynamic: sql.raw, sql.identifier, a parameter in a table position,
// a local fragment.
export async function dynamic(name: string, table: string) {
  await db.execute(sql.raw(`select * from ${name}`));
  await db.execute(sql`select * from ${sql.identifier(table)}`);
  await db.execute(sql`select * from ${table}`);
  const where = sql`true`;
  await db.execute(sql`select * from ${users} where ${where}`);
}

// Row D: an sql value bound to a local is not traced.
export async function viaLocal() {
  const query = sql`select * from ${users}`;
  await db.execute(query);
}

// The reader's refusals: DDL, a view the group does not hold.
export async function outside() {
  await db.execute(sql`create table if not exists scratch (id int)`);
  await db.execute(sql`select * from active_users`);
}
