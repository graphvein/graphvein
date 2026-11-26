# Angular routes

Link-time Angular routing facts (ADR 0036): ui_route nodes, routes_to,
loads_children and contains edges, path composition and angular_route_*
diagnostics, built from the cached router payload (ADR 0035), declaration
constants and the ADR 0031 NgModule scopes.

- `standalone/` is an Angular 21 application (package.json pins core and
  compiler-cli, so components default to standalone). `app.config.ts` holds the
  root provideRouter; `forms.ts` covers aliased, namespaced, type-only,
  other-router, shadowed-callee and Component-providers forms; `local.ts` a
  local provideRouter; `lists.ts` satisfies, spread and mutated const arrays;
  `app.routes.ts` path, loader, guard, resolver, redirect and partial forms;
  `invalid.routes.ts` validateNode (R6) violations; `src/testing/router.spec.ts`
  test-scope registrations of production and test-only routes.
- Revision 1 additions in `standalone/`: production provideRouter is
  a root only inside proven application bootstrap providers. `main.ts`
  bootstraps the imported `appConfig` (corpus shape); `forms.main.ts` covers
  inline options with an aliased `bootstrapApplication`, a local const config
  through a namespaced `createApplication`, nested and spread provider arrays,
  and an unknown option form (`mergeApplicationConfig`, unproven).
  `app.config.ts` adds an unbootstrapped config (unproven) and providers
  bootstrapped only from the spec file (test-scope paths); `route-providers.ts`
  a route-providers const (unproven); `factories.ts` a static forRoot and an
  uncalled function (unproven); `anon.routes.ts` with `anon.mutator.ts` a
  default-export array mutated through a default import; `twins.routes.ts`
  @N numbering that counts an unreached array; `shared.routes.ts` a
  non-standalone component reached eagerly and lazily; `app.routes.ts` a spread
  hiding loadComponent and guards, children plus loadChildren, and
  parenthesized / `as const` path literals. `forms.ts`, `lists.ts` and
  `invalid.routes.ts` registrations that no bootstrap reaches are unproven.
- `ngmodule/` is an Angular 15.2.3 NgModule application bootstrapped with
  platformBrowserDynamic: eager forChild (HomeModule), a module registered both
  eagerly and in a lazy closure (HelpModule), nested lazy modules
  (Settings -> Profile), forRoot in a lazy closure, an unreached module, a lazy
  cycle, provideRouter in NgModule providers, a guard through a barrel and an
  invalid loadChildren target. Revision 1 adds a routing module reached only
  through another module's `exports` (`exported/`), a lazy NgModule whose
  provideRouter sits in a providers const (`lazy-providers/`, unproven, no
  `/lazy-providers/x`) and a lazy NgModule with partial metadata (`partial/`,
  unresolved).

Tests copy the fixture to a temporary directory; they rewrite `app.routes.ts`
and `segments.ts` there to check relinking without reparsing (including that
reaching the first twin does not rename the second), and write a CRLF copy to
check identical facts. No inert ignore-file mappings are needed: the
fixture contains no ignore files or ignored directory names.

The golden projects only ui_route nodes, routing edges (routes_to,
loads_children, contains into ui_route) and angular_route_* diagnostics.
