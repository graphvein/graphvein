import { customType, text, timestamp } from "drizzle-orm/pg-core";

// Not a schema module: column helpers the schema imports.
export const bytea = customType<{ data: Uint8Array }>({
  dataType() {
    return "bytea";
  },
  toDriver(value) {
    return value;
  },
});

export const wideType = customType<{ data: string }>({
  dataType() {
    return "text";
  },
  notRead: 1,
});

// A spread object of columns: every table that spreads it shares these
// builders.
export const timestamps = {
  createdOn: timestamp("created_on").defaultNow().notNull(),
  slug: text("slug").unique(),
};

// customType dataType() literals: text is one the oracle compared (DR-C3);
// varchar(255) is spelled character varying(255) by Postgres, so its type
// is unknown (G2B5).
export const plainText = customType<{ data: string }>({
  dataType() {
    return "text";
  },
});

export const shortText = customType<{ data: string }>({
  dataType() {
    return "varchar(255)";
  },
});
