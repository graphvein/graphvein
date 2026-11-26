import { integer, pgTable, text } from "../../lib/core";

// Builders through a local re-export: no model payload, so these tables
// are not read; one marker names the file and the group's table list is
// may_be_incomplete.
export const viaCore = pgTable("via_core", { id: integer("id").primaryKey(), note: text("note") });
