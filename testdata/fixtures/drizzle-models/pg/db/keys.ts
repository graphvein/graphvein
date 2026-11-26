import { relations, sql } from "drizzle-orm";
import {
  check,
  foreignKey,
  index,
  integer,
  pgTable,
  primaryKey,
  text,
  timestamp,
  unique,
  uniqueIndex,
  uuid,
  varchar,
} from "drizzle-orm/pg-core";
import { plainText, shortText } from "../lib/shared";
import { logLevel } from "./enums/mood";
import { kinds, rls } from "./schema";

// Inline foreign keys (DR-C2): a table in another file with actions, a
// self reference, a target the group does not claim.
export const authors = pgTable("authors", {
  id: integer("id").primaryKey(),
  kindId: integer("kind_id").references(() => kinds.id, { onDelete: "cascade", onUpdate: "set null" }),
  mentorId: integer("mentor_id").references(() => authors.id),
  rlsId: integer("rls_id").references(() => rls.id),
  email: text("email").notNull(),
});

// Extra config as an array (DR-X1): composite primary key, unique
// constraints, a composite-capable foreignKey, indexes of every form,
// a check.
export const posts = pgTable(
  "posts",
  {
    authorId: integer("author_id").notNull(),
    slug: varchar("slug", { length: 40 }).notNull(),
    title: text("title"),
    body: text("body"),
    publishedAt: timestamp("published_at"),
    score: integer("score"),
  },
  (t) => [
    primaryKey({ columns: [t.authorId, t.slug] }),
    unique().on(t.title, t.body).nullsNotDistinct(),
    unique("posts_slug_key").on(t.slug),
    foreignKey({ columns: [t.authorId], foreignColumns: [authors.id] }).onDelete("cascade"),
    index().on(t.publishedAt.desc(), t.score.nullsFirst()),
    index("posts_title_idx").on(t.title),
    uniqueIndex("posts_lower_title_idx").on(sql`lower(${t.title})`),
    uniqueIndex("posts_title_live_idx").on(t.title).where(sql`${t.publishedAt} is not null`),
    index("posts_recent_idx").using("brin", t.publishedAt).where(sql`${t.publishedAt} is not null`).concurrently(),
    index("posts_body_idx").using("btree", t.body.op("text_pattern_ops")).with({ fillfactor: 70 }),
    check("posts_score_check", sql`${t.score} >= 0`),
  ],
);

// The deprecated object form, a named composite foreign key.
export const comments = pgTable(
  "comments",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    postAuthor: integer("post_author").notNull(),
    postSlug: varchar("post_slug", { length: 40 }).notNull(),
  },
  (t) => ({
    postRef: foreignKey({
      name: "comments_post_fk",
      columns: [t.postAuthor, t.postSlug],
      foreignColumns: [posts.authorId, posts.slug],
    }).onUpdate("cascade"),
    bySlug: index("comments_slug_idx").on(t.postSlug),
  }),
);

// A generated foreign key name longer than 63 bytes.
export const longChild = pgTable("a_very_long_child_table_name_for_truncation", {
  parentReferenceIdentifier: integer("parent_reference_identifier").references(() => authors.id),
});

// Row D elements: the positional primaryKey form, a block body.
export const oddExtra = pgTable("odd_extra", { a: text("a"), b: text("b") }, (t) => [
  index("odd_extra_a_idx").on(t.a),
  primaryKey(t.a, t.b),
]);
export const blockExtra = pgTable("block_extra", { a: text("a") }, (t) => {
  return [index("block_extra_a_idx").on(t.a)];
});

// DR-P1: a column the extra config or a reference names that the table
// does not have; a unique name used by a column and an element.
export const badIndex = pgTable("bad_index", { a: text("a") }, (t) => [index("bad_index_idx").on(t.nope)]);
export const badRef = pgTable("bad_ref", { a: integer("a").references(() => authors.nope) });
export const dupUnique = pgTable("dup_unique", { a: text("a").unique("du"), b: text("b") }, (t) => [unique("du").on(t.b)]);

// Foreign key actions, index forms, a mixed-case enum and customType
// literals the corpora use (G2B5).
export const audits = pgTable(
  "audits",
  {
    id: integer("id").primaryKey(),
    authorId: integer("author_id").references(() => authors.id, { onDelete: "restrict" }),
    kindId: integer("kind_id").references(() => kinds.id, { onDelete: "set null", onUpdate: "cascade" }),
    note: text("note"),
    code: text("code"),
    level: logLevel("level"),
    body: plainText("body"),
    short: shortText("short"),
  },
  (t) => [
    index("audits_note_idx").on(t.note).where(sql`${t.note} is not null`),
    uniqueIndex("audits_code_idx").on(t.code),
  ],
);

// ORM relations (DR-R1): never foreign keys.
export const writer = authors;
export const authorsRelations = relations(writer, ({ one, many }) => ({
  kind: one(kinds, { fields: [authors.kindId], references: [kinds.id] }),
  posts: many(posts),
  mentor: one(authors, { fields: [authors.mentorId], references: [authors.id], relationName: "mentor" }),
  hidden: many(rls),
  odd: "not a relation",
}));
export const commentsRelations = relations(comments, () => ({}));
export const postsRelations = relations(posts, (h) => ({
  author: h.one(authors, { fields: [posts.authorId], references: [authors.id] }),
}));
