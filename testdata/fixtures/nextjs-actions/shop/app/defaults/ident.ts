'use server';

// Row SA-M1, default_clause (T024): `export default a` of a
// local async function.
async function publishNote(id: number) {
  return id;
}

export default publishNote;
