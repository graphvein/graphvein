# ADR 0092: Reading Rust files with attributes on struct pattern fields

**Status:** Accepted (delegated review, 2026-11-27)

## Context

Row RS-F2 of the Rust structure contract (`docs/lang/rust-structure.md`,
ADR 0071) gives a file in which tree-sitter reports a syntax error nothing
beyond its file node. The rule exists because a tree with an error is not a
tree Rust would build: the investigation found claims in a file the Rust
parser rejects (class D-REJECTED, stop PS-S4-9), and a recovered tree can
mis-delimit items, so neither its declarations nor its `mod` items are
certain.

Rust allows outer attributes on the fields of a struct pattern (the
reference's `StructPatternField` takes `OuterAttribute*`):

```rust
let AppServices {
    wake,
    #[cfg(not(target_arch = "wasm32"))]
    watch,
} = services;
```

tree-sitter-rust 0.24.2, the pinned grammar and also the latest upstream
release, has no rule for them (`field_pattern` takes no attributes) and
reports ERROR. A real egui workspace keeps its application type and the
declarations of 13 modules in such a file: RS-F2 dropped all of it, so those
modules were reached from no crate root.

## Options

1. **Patch the grammar.** Add `repeat($.attribute_item)` to the field
   pattern rule in a digest-checked patch under `grammars/patches/` and
   rebuild the Rust module with the `Grammar assets` workflow. This is the
   right long-term fix, but the module is a reviewed CI artifact (WASI SDK,
   tree-sitter CLI) that cannot be built offline, and the upstream grammar
   has no fix to take.
2. **Narrow RS-F2 to the items that do not overlap an ERROR node.** Keeps
   everything else in any file with an error. It reopens D-REJECTED: a file
   with a real syntax error would claim items Rust never reads, and a
   mis-lexed literal can make text that is not code look like items outside
   every ERROR node.
3. **Read the file from a verified rewrite (chosen).** Replace the candidate
   attributes by spaces, keeping line ends so every byte offset stays, parse
   the copy again and read it only when the result proves the original was
   valid in that place.
4. **Leave it.** Keeps the coverage loss.

## Decision

Option 3, as row RS-F4. For a Rust file without a lone CR in which
tree-sitter reports a syntax error:

- Candidates are the `#[...]` runs (bracket-matched, `"..."` strings
  skipped) inside the nodes that hold the file's ERROR nodes. A file whose
  error has no ERROR node (only MISSING tokens) has none.
- Each candidate is parsed alone as the attribute of a struct field; one
  that does not parse as exactly that attribute is not replaced.
- The file is parsed again with the candidates replaced by spaces. It is
  read only when that tree has no error at all and every replaced run lies
  directly inside a `struct_pattern`, after its `{` or a `,`, before a
  `field_pattern`, with only white space between. If the tree is clean but
  some runs are elsewhere (an attribute on a statement near the error),
  those are restored and the file is parsed once more under the same
  checks. An attribute before the `..` rest pattern does not count: rustc
  1.97 rejects it.
- The rows read the clean tree with every node's text taken from the
  original bytes, so names, spans and hashes are those of the file. The
  replaced attributes claim nothing (attributes are not modelled and cfg is
  never evaluated). Any other outcome leaves the file to RS-F2.

The original file is the clean tree with attributes inserted where Rust
allows them, so it is valid Rust wherever the tree says so, and the oracle
can compare it like any other file (syn reads field pattern attributes).
The second parse runs after every first tree is released, through the
indexer's bounded pipeline, and only for the rare files that have such
candidates.

## Consequences

- Files with this grammar gap claim their declarations, `mod` and `use`
  edges, calls and framework payloads again. The contract gains row RS-F4;
  RS-F2 covers the files RS-F4 does not read. Extractor version
  extractors-63.
- A file with both a pattern field attribute and a real syntax error stays
  RS-F2.
- Up to three extra parse jobs per affected file (the probes, the blanked
  copy, possibly the restored subset); no cost for other files.

## Known gaps

- Other tree-sitter-rust gaps (attributes in other positions the grammar
  lacks, a NUL byte in a line comment or character literal, which the
  grammar reports as an error) still go to RS-F2.
- The grammar patch (option 1) remains the better fix; when the Rust module
  is rebuilt with it, RS-F4 stops reaching these files and can be removed.
