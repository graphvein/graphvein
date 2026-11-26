import type { ColumnOptions, ColumnType } from "typeorm";
import { Column } from "typeorm";

const isPgsql = process.env.DB_TYPE === "postgres";

export function resolveDbType(type: ColumnType): ColumnType {
  return isPgsql && type === "datetime" ? "timestamp with time zone" : type;
}

// A TO-C6 wrapper: only the type is rewritten before Column(options).
export function DbAwareColumn(columnOptions: ColumnOptions) {
  if (columnOptions.type) {
    columnOptions.type = resolveDbType(columnOptions.type);
  }
  return Column(columnOptions);
}

// Not a TO-C6 wrapper: it renames the column (TO-P2).
export function RenamedColumn(columnOptions: ColumnOptions) {
  columnOptions.name = "renamed";
  return Column(columnOptions);
}
