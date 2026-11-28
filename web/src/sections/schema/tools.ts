// Plain names of the schema tools the server sends by id: the unread
// migration tools of unread_tools and the model tools of refused_sources
// (T070). An id this table does not know is shown as sent.

import type { RefusedSource } from "@/api/schema";

const LABELS: Record<string, string> = {
  alembic: "Alembic",
  "prisma-migrate": "Prisma",
  "drizzle-kit": "Drizzle",
  "sequelize-migrations": "Sequelize",
  "typeorm-migrations": "TypeORM",
  "diesel-migrations": "Diesel",
  "gorm-v1": "GORM v1",
  gorm: "GORM",
  prisma: "Prisma",
  drizzle: "Drizzle",
  sequelize: "Sequelize",
  typeorm: "TypeORM",
  diesel: "Diesel",
};

/** toolLabel is a schema tool's name for a reader ("Prisma" for
 * prisma-migrate, "TypeORM" for typeorm-migrations, "GORM v1" for
 * gorm-v1); an unknown tool is shown as sent. */
export function toolLabel(tool: string): string {
  return LABELS[tool] ?? tool;
}

/** MIGRATION_TOOLS are the unread_tools ids that name migrations (every
 * other id the server sends names models: gorm-v1). */
export const MIGRATION_TOOLS = new Set(["alembic", "prisma-migrate", "drizzle-kit", "sequelize-migrations", "typeorm-migrations", "diesel-migrations"]);

/** refusalReason is a refusal's stored message without the leading
 * "<file>: " or "<file>:<line>: " the linkers write, so the page does not
 * repeat the path it shows beside it; any other message is kept whole. */
export function refusalReason(r: RefusedSource): string {
  const m = r.message;
  if (m.startsWith(`${r.file}: `)) return m.slice(r.file.length + 2);
  const line = /^(\d+): /.exec(m.startsWith(`${r.file}:`) ? m.slice(r.file.length + 1) : "");
  if (line !== null) return `line ${line[1] ?? ""}: ${m.slice(r.file.length + 1 + line[0].length)}`;
  return m;
}

/** refusalText is one refusal as a sentence: "<Tool> models found in
 * <file>, not read: <reason>". */
export function refusalText(r: RefusedSource): string {
  return `${toolLabel(r.tool)} models found in ${r.file}, not read: ${refusalReason(r)}`;
}

/** joinNames lists names in reading order: "A", "A and B", "A, B and C". */
export function joinNames(list: string[]): string {
  if (list.length <= 1) return list.join("");
  return `${list.slice(0, -1).join(", ")} and ${list[list.length - 1] ?? ""}`;
}
