'use server';

import { validated } from '@/lib/validated';
import { helperAction } from '@/lib/helper-actions';

// Row SA-M1: every action export form.
export async function createItem(data: FormData) {
  return data.get('name');
}

export const deleteItem = async (id: number) => id;

export const renameItem = async function (id: number, name: string) {
  return `${id}:${name}`;
};

export const updateItem = validated(async (data) => data.get('id'));

const archiveLocal = async (id: number) => id;
export { archiveLocal as archiveItem };

// An imported binding exported again is a server reference too
// (form imported_clause); `export { a } from` is a build error (D below).
export { helperAction };

export default async function restoreItem(id: number) {
  return id;
}

// Row D: not actions (Next.js build errors or forms the row does not read).
export const LIMIT = 10;
export function syncHelper() {
  return 1;
}
export class Store {}
export * from './more';
export { validated } from '@/lib/validated';

// Types are not values; nothing is recorded for them.
export type ItemId = number;
