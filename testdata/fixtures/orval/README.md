T012e fixture. All routes are under /t012e. Each config has a uniquely named
package. No inert-to-live mappings or ignored directories are needed.
Generated source is absent except in scanned/.

The shared spec contains path, query, body, multiple-content-type and
absent-operationId cases. proven/ covers named, aliased and namespace requests,
all five scheduling APIs, imported/local QueryClient proof, hooks, queryFn
overrides, key/URL exclusions, assigned/spread options, class shadowing/type-only imports,
wrong-file imports and a test-scope caller. Other configs isolate unsupported
settings/version, hook mutators, absent mutators, pathname rewriting and
scanned-source precedence. alias/ is nomutator/ whose app imports the
not-yet-generated client through a tsconfig `paths` alias (`~api/*`): the
prediction uses the compiler's paths target. The root
package.json declares express for server.ts. arrow/ and function-expression/ prove const-bound
mutators with direct fetch forwarding, including generic await fetch. shadow-fetch/
proves that a local shadowed function is excluded when counting global transports.

url-objects.ts covers assignment-derived and literal origins, unknown bases,
pathname/searchParams writes, dot and encoded-dot segments, relative and network
paths, one and two serialization aliases, host/hostname/port assignments,
conditional writes, escapes, shadowed URL, invalid percent escapes, whitespace
and a module-level URL binding. Five of twenty URL-object calls link; fifteen
remain dynamic. Every call and target is visible in orval.golden.json.
