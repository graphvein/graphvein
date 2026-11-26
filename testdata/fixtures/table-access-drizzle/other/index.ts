import { db } from "../src/db/client";
import { users } from "../src/db/schema";

// A package without a Drizzle group: a table const the resolver proves is
// claimed (W7, Revision 4); a relational query needs the group's schema
// binding and is unknown.
export async function elsewhere() {
  return db.select().from(users);
}

export async function elsewhereQuery() {
  return db.query.users.findMany();
}
