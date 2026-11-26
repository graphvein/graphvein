import Form from 'next/form';
import saveNote from './arrow';
import publish from './ident';
import checked from './call';

// A component with a prop named action: passing an action to it is a
// prop (row D), not a form use (T024).
function Panel({ action }: { action: (data: FormData) => Promise<unknown> }) {
  return <span>{String(action)}</span>;
}

// Uses of the default actions (row SA-C1): intrinsic form elements and
// next/form's Form.
export default function DefaultsPage() {
  return (
    <form action={saveNote}>
      <button formAction={publish}>publish</button>
      <input type="submit" formAction={checked} />
      <Form action={publish}>
        <Panel action={saveNote} />
      </Form>
    </form>
  );
}
