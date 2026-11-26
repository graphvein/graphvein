import { DataSource } from "typeorm";
import { Account } from "./entity/Account";
import { Archive } from "./entity/Archive";
import { Holder } from "./entity/Holder";
import { Member } from "./entity/Member";
import { Note } from "./entity/Note";
import { Odd } from "./entity/Odd";
import { Profile } from "./entity/Profile";
import { Tag } from "./entity/Tag";
import { Tagged } from "./entity/Tagged";

// The options selector of TO-K1: the dialect is chosen at run time
// (sqlite or postgres), so dialect-dependent facts are per dialect.
const entities = [Account, Profile, Note, Archive, Tagged, Holder, Member, Tag, Odd];

const sqliteConfig = { type: "sqlite", database: "app.sqlite", entities };
const postgresConfig = { type: "postgres", url: "postgres://localhost/app", entities };

function getDataSource() {
  if (process.env.DB_TYPE === "postgres") {
    return postgresConfig;
  }
  return sqliteConfig;
}

export const dataSource = new DataSource(getDataSource());
