# Pipe precedence

The root package uses Angular 21 implicit standalone declarations; legacy/ uses
Angular 15 implicit non-standalone declarations. All inputs are synthetic.
No ignore-file or ignored-directory name mappings are needed.

After uses the same pipe twice, checking usage counts versus merged edges.
Before, Repeat and FirstContribution retain the first contributing position.
RepeatedExternalPackage keeps two external leaves with the same package target.
OpenUnit and OpenFirstUnit remain shadowable regardless of internal export order.
ClosedUnit, SpreadAfter, ForwardAfter and ObjectAfter check ordered expansion.
DeclaringModule checks external ModuleWithProviders followed by declarations.
Duplicate names, unknown names and invalid contexts preserve their old statuses.
