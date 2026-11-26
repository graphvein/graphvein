'use client';

import { useActionState } from 'react';
import { t016dSave, t016dPassed } from './actions';

function T016dChild({ onRun }: { onRun: () => Promise<number> }) {
  return <button onClick={() => void onRun()}>run</button>;
}

export function T016dForm() {
  const [state, save] = useActionState(t016dSave, null);
  const retry = () => t016dSave(new FormData());
  return (
    <form action={save}>
      {String(state)}
      <button onClick={retry}>retry</button>
      <T016dChild onRun={t016dPassed} />
    </form>
  );
}
