'use client';

import * as React from 'react';
import { useActionState, startTransition } from 'react';
import { useFormState } from 'react-dom';
import { deleteItem, updateItem, renameItem, createItem, helperAction } from '@/app/actions';
import { sharedRemove } from '@/lib/shared';
import * as actions from '@/app/actions';
import { removeAll } from '@/lib/client-helper';
import { barrelRemove } from '@/lib/barrel-target';
import { starRemove } from '@/lib/star-target';
import type { Shape } from '@/lib/types-only';
import { Point } from '@/lib/shapes';

export function ItemForm({ onSave }: { onSave: (d: FormData) => Promise<unknown> }) {
  // Row SA-C2: useActionState from react, useFormState from react-dom.
  const [state, update] = useActionState(updateItem, null);
  const [old, create] = useFormState(createItem, null);
  const [viaNs] = React.useActionState(renameItem, null);
  // Not an action: a client function passed to the hook.
  const [local] = useActionState(async () => 1, null);
  // A conditional first argument links to each action (R1); row D: a
  // member, a namespace read, an action passed as an argument.
  const [either] = useActionState(state ? updateItem : createItem, null);
  const bound = deleteItem.bind(null, 1);
  actions.deleteItem(2);
  startTransition(deleteItem as never);
  // Row SA-C3: a direct call in a 'use client' file (boundary client).
  const onClick = () => deleteItem(3);
  const viaReexport = () => helperAction(5);
  // A parameter shadows the import: no use.
  const shadow = (deleteItem: (n: number) => void) => deleteItem(4);
  const p: Shape | Point | null = null;
  return (
    <form action={update}>
      {String(state)}{String(old)}{String(viaNs)}{String(local)}{String(either)}
      <button onClick={onClick} formAction={create}>go</button>
      {String(bound)}{String(shadow)}{String(p)}{String(removeAll)}{String(onSave)}{String(viaReexport)}{String(sharedRemove)}{String(barrelRemove)}{String(starRemove)}
    </form>
  );
}
