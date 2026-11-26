import { subscribe, signup } from './actions';

export default function Page() {
  return (
    <form action={subscribe}>
      <input name="email" />
      <button formAction={signup}>Sign up</button>
    </form>
  );
}
