# nextjs-actions fixture (T024b)

Server actions in a Next.js project (`shop/`, its `package.json` declares
`next`). Every row of the server action claim contract
(docs/extract/server-actions.md) and its default branch D is reached;
TestServerActionsContract checks that. No file needs an inert name.

- `shop/app/actions.ts`: a `'use server'` module with every SA-M1 export
  form and the refused ones (D).
- `shop/app/items/page.tsx`: a server component with inline actions
  (SA-I1, including an anonymous one), form uses (SA-C1), a server-side
  direct call (SA-C3, boundary unknown) and D uses.
- `shop/components/item-form.tsx`: a `'use client'` file with
  `useActionState` / `useFormState` (SA-C2), a direct call (SA-C3,
  boundary client), a shadowing parameter and D uses.
- `shop/lib/client-helper.ts`: no directive, reached from the client file
  through a value import (SA-C3, boundary client, `client_entry`);
  `shop/lib/types-only.ts` and `shop/lib/shapes.ts` are reached only for
  types (boundary unknown).
- `shop/lib/shared.ts` (R1): imported by the server page and the client
  form, so its direct call is boundary unknown (`shared_module`).
- `shop/lib/helper-actions.ts` (R1): exported again by `app/actions.ts`
  (form `imported_clause`); the client form's call through that file links.
- `shop/lib/barrel/index.ts` (T024): a barrel with `export { a }
  from`, `export *` and `export type { T } from`; `shop/app/srv/page.tsx`
  reaches `shop/lib/barrel-target.ts` and `shop/lib/star-target.ts` only
  through it while the client form imports them directly, so their direct
  calls are boundary unknown (`shared_module`).
- `shop/app/defaults/` (T024): `'use server'` files with the
  default export forms `default_async_arrow`, `default_clause`,
  `default_call` and a refused sync arrow (D); `page.tsx` uses them on an
  intrinsic `<form>`, `<button>`, `<input>` and next/form's `Form`, and
  passes one to a component's `action` prop (D, counted).
- `shop/app/both.ts`, `shop/app/bad-client.tsx`: build errors (D).
- `plain/`: a `'use server'` file outside any Next.js project (nothing).
