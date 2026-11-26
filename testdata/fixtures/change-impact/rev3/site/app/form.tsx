'use client';

import { useActionState } from 'react';
import { saveNote, pinNote } from './actions';

export function NoteForm() {
  const [state, save] = useActionState(saveNote, null);
  const pin = () => pinNote(1);
  return <form action={save}>{String(state)}<button onClick={pin}>pin</button></form>;
}
