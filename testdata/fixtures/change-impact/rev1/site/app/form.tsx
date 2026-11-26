'use client';

import { useActionState } from 'react';
import { saveNote, dropNote } from './actions';

export function NoteForm() {
  const [state, save] = useActionState(saveNote, null);
  const drop = () => dropNote(1);
  return <form action={save}>{String(state)}<button onClick={drop}>drop</button></form>;
}
