import { isNotNull, relations } from "drizzle-orm";
import { integer, pgSchema, pgTable, pgView, primaryKey, serial, text, timestamp } from "drizzle-orm/pg-core";
import { profiles } from "./extra";

export const users = pgTable("users", {
  id: serial("id").primaryKey(),
  email: text("email").notNull().unique(),
  name: text("name"),
  managerId: integer("manager_id"),
});

export const posts = pgTable("posts", {
  id: serial("id").primaryKey(),
  authorId: integer("author_id")
    .notNull()
    .references(() => users.id),
  title: text("title").notNull(),
  publishedAt: timestamp("published_at"),
});

export const comments = pgTable("comments", {
  id: serial("id").primaryKey(),
  postId: integer("post_id")
    .notNull()
    .references(() => posts.id),
  body: text("body").notNull(),
});

export const tags = pgTable("tags", {
  id: serial("id").primaryKey(),
  label: text("label").notNull().unique(),
});

export const postTags = pgTable(
  "post_tags",
  {
    postId: integer("post_id")
      .notNull()
      .references(() => posts.id),
    tagId: integer("tag_id")
      .notNull()
      .references(() => tags.id),
  },
  (t) => [primaryKey({ columns: [t.postId, t.tagId] })],
);

// A table of its own Postgres schema: rendered "audit"."log" in sql.
export const audit = pgSchema("audit");

export const auditLog = audit.table("log", {
  id: serial("id").primaryKey(),
  action: text("action").notNull(),
  userId: integer("user_id"),
});

// A view is no table (CT-Z1 refusal).
export const activeUsers = pgView("active_users").as((qb) => qb.select().from(users).where(isNotNull(users.name)));

export const usersRelations = relations(users, ({ one, many }) => ({
  posts: many(posts),
  manager: one(users, { fields: [users.managerId], references: [users.id] }),
  profile: one(profiles, { fields: [users.id], references: [profiles.userId] }),
}));

export const postsRelations = relations(posts, ({ one, many }) => ({
  author: one(users, { fields: [posts.authorId], references: [users.id] }),
  comments: many(comments),
  postTags: many(postTags),
}));

export const commentsRelations = relations(comments, ({ one }) => ({
  post: one(posts, { fields: [comments.postId], references: [posts.id] }),
}));

export const postTagsRelations = relations(postTags, ({ one }) => ({
  post: one(posts, { fields: [postTags.postId], references: [posts.id] }),
  tag: one(tags, { fields: [postTags.tagId], references: [tags.id] }),
}));
