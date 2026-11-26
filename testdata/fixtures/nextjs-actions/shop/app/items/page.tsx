import { createItem, archiveItem } from '../actions';
import restore from '../actions';
import { ItemForm } from '@/components/item-form';
import { sharedRemove } from '@/lib/shared';
import { helperAction } from '@/lib/helper-actions';

// A server component (no directive): inline actions and form uses.
export default function ItemsPage({ flag }: { flag: boolean }) {
  // Row SA-I1, named_function; its call of createItem is SA-C3 with
  // boundary unknown (a server-side call).
  async function saveInline(data: FormData) {
    'use server';
    await createItem(data);
  }
  // Row SA-I1, const_arrow.
  const quickSave = async () => {
    'use server';
  };
  // Row SA-I1, let_binding (Next.js reads any binding); a reassigned one
  // keeps its node, but its uses are not linked.
  let mutable = async () => {
    'use server';
  };
  let changing = async () => {
    'use server';
  };
  changing = async () => undefined;
  // Row D: a sync function.
  function notAsync() {
    'use server';
  }
  return (
    <form action={saveInline}>
      <button formAction={quickSave}>quick</button>
      <button formAction={archiveItem}>archive</button>
      <button formAction={restore}>restore</button>
      {/* Row SA-I1 anonymous, linked as the form's action (R1). */}
      <form action={async () => { 'use server'; }} />
      {/* A conditional value links to each action (R1); a prop is D. */}
      <form action={flag ? createItem : saveInline} />
      <ItemForm onSave={createItem} />
      <button formAction={mutable}>mutable</button>
      <button formAction={changing}>changing</button>
      <span>{String(notAsync)}{String(sharedRemove)}{String(helperAction)}</span>
    </form>
  );
}
