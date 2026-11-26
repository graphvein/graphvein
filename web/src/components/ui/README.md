# shadcn/ui components

These files are copied from shadcn/ui, not installed as a package:

- Source: https://github.com/shadcn-ui/ui, commit
  `4aaea19d9cff3e0908e12a8c1d504b8c5ec34610`, directory
  `apps/v4/registry/new-york-v4/ui/`.
- Files: `button.tsx`, `badge.tsx`, `tooltip.tsx`, `separator.tsx`,
  `skeleton.tsx`.
- Fetched from `raw.githubusercontent.com` (the `shadcn` CLI needs
  `ui.shadcn.com`, which the build container cannot reach).
- Only change: the registry's placeholder import `import { cn } from "cn"`
  is rewritten to `import { cn } from "@/lib/utils"`, as the shadcn CLI
  does. Everything else is unchanged; the look is adjusted through the
  tokens in `src/styles/tokens.css` and `className` at the call sites.

T016d2 (API section) copied no further component: Radix Dialog / Sheet
inject `<style>` elements (react-remove-scroll) that the page's CSP blocks,
and ScrollArea renders an inline `<style>`; the gap sheet uses the native
`<dialog>` element instead (ADR 0042).

Licence (from `LICENSE.md` at that commit; the notice is also in the
generated `third-party-licenses.txt` the binary serves):

```
MIT License

Copyright (c) 2023 shadcn

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
