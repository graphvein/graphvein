import { posts, users } from "./schema";

// A module-level alias const (kaneo's `export const user = userTable`).
export const members = users;

// A const object of table consts (kaneo's `export const schema = { ... }`).
export const tablesByName = { users, posts };
