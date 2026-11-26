# Python fixture

Hand-checked declarations and links cover a package, relative and absolute
imports, module constants, decorated classes and methods, async and nested
definitions, overloads, conditional redefinitions and syntax recovery.
`pkg/broken.py` is deliberately invalid. No inert ignore-file mappings are needed.
Tests copy this tree to a temporary non-Git root.
# T007b coverage

`pkg/main.py` calls `pkg.util.help`, aliased `utility.help`, from-imported
module `util.help`, absolute from-imported `tools.help`, local `User.validate`,
module class `util.Helper.method`, and from-imported class `Imported.method`.
`print` is external/builtin; `missing` has no binding and is unknown. Multiple
source calls to one target merge their evidence into one contract edge.
