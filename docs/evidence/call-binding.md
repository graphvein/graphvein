# Call binding of TypeScript and JavaScript calls

This page states how often a TypeScript or JavaScript call edge that
Graphvein emits points at the wrong function, checked against the
language's own scope analysis. The binding rules, their revisions and the
review notes with these numbers are in
[ADR 0011](../adr/0011-typescript-javascript-object-owners-and-repeated-declarations.md)
("Review notes (delegated review, 2026-11-25, T046)"); the claim table is
[TypeScript resolution](../lang/typescript-resolution.md).

## Result

**0 wrong and 0 undecidable call edges on all 12 corpora**, measured with
the aggregate extractor version `extractors-61`. The fixture's cases were
61 of 61 right.

Before the call binding revisions of ADR 0011, the same comparison found 605 wrong
call edges. Each later revision was measured again on the 12 corpora:

| revision | wrong edges removed | right edges regained | right edges lost |
|---|---|---|---|
| round 1 | the 605 above | | |
| round 2 (written bindings) | 154 more (owncast 120, seerr 27, langflow 6, kaneo 1) | 41 | |
| round 3 (member writes) | 1,423 more (owncast 1,329) | 989 | |
| round 4 (literal `Object.assign` keys) | | 26 (kaneo) | none |
| round 5 (receivers of unknown owner) | 3,766 more (owncast 3,683, seerr 67, langflow 11, angular-ngmodule 3, dozzle 2) | | 6 (seerr) |
| gate round (member calls through imports) | no corpus site changed | | |

## Method

- **Oracle.** acorn 8.14.0 with eslint-scope 8.2.0 for JavaScript, and
  @typescript-eslint 8.74.0 with typescript 5.9.3 for TypeScript. Every call
  Graphvein resolved is compared with the language's own scope analysis
  and rated right, wrong or undecidable (for example a call under `with`
  or a direct `eval`).
- **What is counted.** Wrong edges, not missing ones: this is a precision
  measurement. A call Graphvein cannot bind stays unresolved and is not
  counted here.
- **Corpora.** 12 corpora: 11 public open-source repositories at the pins
  in [`scripts/cloud-corpus.sh`](../../scripts/cloud-corpus.sh) and
  Graphvein's own web UI (`web/`). owncast (including its committed
  Next.js build output), langflow (`src/frontend`), seerr,
  angular-ngmodule, dozzle (`assets`), kaneo, dokploy (`apps/dokploy`),
  react, angular, express and chatbot-ui.

## Limitations

- The comparison checks the callee binding only. Calls left unresolved
  cost recall, and the main ones are listed: repeated qualified names stay
  unresolved (about 2,440 sites the oracle rates right, mostly in minified
  bundles), subclass overrides get no dispatch edge, and a file that writes
  `target[key] = value` through a receiver of unknown owner loses its
  member-call edges. See [known gaps](../known-gaps.md#typescript-and-javascript-call-binding).
- The harness is over-strict on overload signatures, which can only rate a
  right edge wrong, never the reverse.
- The oracle script is not part of the public tree; the numbers above are
  the ones recorded in ADR 0011's review notes.
