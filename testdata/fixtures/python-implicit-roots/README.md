# Python implicit roots

Source-authored import/call/route expectations cover a Django-like tree, a
two-level package, namespace parents, explicit precedence, repository-name
collisions, unresolved flat sibling scripts and repository src without metadata.
Only explicit/ has pyproject.toml. Package initializers are empty unless they
declare the collision target. The Django-like app also exercises shared FastAPI
object, handler and constant-prefix resolution through absolute imports.

No inert ignore-name mappings are needed: every fixture path is directly
scannable and none is ignored. Tests copy this tree to temporary storage before
adding/removing initializers; corpus checkouts are never modified.
