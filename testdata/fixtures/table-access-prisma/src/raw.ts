import { Prisma } from "@prisma/client";
import { prisma } from "./db";

// CT-R3: raw SQL through the reader against the Prisma group's tables.
export async function raw(id: number, query: Prisma.Sql) {
  await prisma.$queryRaw`SELECT p.id FROM "Post" p JOIN "User" u ON u.id = p."authorId" WHERE u.id = ${id}`;
  await prisma.$executeRaw`UPDATE "Tag" SET name = ${"x"} WHERE id IN (SELECT "B" FROM "_PostToTag" WHERE "A" = ${id})`;
  await prisma.$queryRawUnsafe('SELECT * FROM comments WHERE id = $1', id);
  await prisma.$executeRawUnsafe(`DELETE FROM user_groups WHERE id = $1`, id);
  await prisma.$queryRaw`INSERT INTO "Tag" (name) VALUES (${"a"}) ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name`;
  await prisma.$queryRaw`SELECT * FROM "User"\nWHERE id = ${id}`;
  await prisma.$queryRaw`SELECT 1`;
}

// CT-R3 refusals.
export async function rawRefusals(id: number, query: Prisma.Sql) {
  await prisma.$queryRaw`SELECT * FROM "Post" WHERE ${Prisma.sql`id = ${id}`}`;
  const where = Prisma.sql`id = ${id}`;
  await prisma.$queryRaw`SELECT * FROM "Post" WHERE ${where}`;
  await prisma.$queryRaw(query);
  await prisma.$executeRawUnsafe(`SET search_path TO "${id}"`);
  await prisma.$executeRawUnsafe('TRUNCATE "Tag"');
  await prisma.$queryRaw`SELECT * FROM missing_table`;
}
