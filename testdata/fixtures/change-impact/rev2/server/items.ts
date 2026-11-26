export function formatRow(row: string) {
  return row.trim().toUpperCase();
}

export function formatAll(rows: string[]) {
  return [formatRow(rows[0])];
}

export function loadItems() {
  return formatAll(['a', 'b']);
}

export class Formatter {
  static first(rows: string[]) {
    return formatRow(rows[0]);
  }
}
