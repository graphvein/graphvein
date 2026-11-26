# Static class calls

Own static method resolution through local, imported, aliased, default, barrel
and namespace bindings. `mutations.ts` invalidates imported members across files;
the unrelated `Assigned.safe` member remains eligible. Calls deliberately placed
at file scope exercise the new path without changing legacy resolution.

The fixture includes invalid TypeScript to check conservative extraction (private
access, instance/field/accessor/repeated/ambient/merged calls). Such code is never
executed. `/t012f/wrapped` verifies an imported static URL forwarding wrapper.
All names are literal; no inert-to-live ignore or directory mapping is needed.
