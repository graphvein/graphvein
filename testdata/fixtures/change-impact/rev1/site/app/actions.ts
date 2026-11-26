'use server';

export async function saveNote(data: FormData) {
  return data.get('note');
}

export async function dropNote(id: number) {
  return id;
}
