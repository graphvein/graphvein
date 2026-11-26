import { DataSource } from "typeorm";
import { Book } from "./entity/Book";
import { Item } from "./entity/Item";
import { Reader } from "./entity/Reader";
import { Shelf } from "./entity/Shelf";

export const dataSource = new DataSource({ type: "better-sqlite3", database: "v1.sqlite", entities: [Item, Shelf, Book, Reader] });
