package routes

// SetAuthTag installs the claim-branch hook for the external tests, which
// index the fixture through internal/index (an import cycle for an
// in-package test).
func SetAuthTag(f func(row string)) { authTag = f }

// SetExpressPathTag installs the Express path claim hook (T028) for the
// external tests.
func SetExpressPathTag(f func(row string)) { expressPathTag = f }

// SetShapeTag installs the route shape claim hook of the Express (T029
// SH-E) and FastAPI (T029 SH-F) shape rows for the external tests.
func SetShapeTag(f func(row string)) { shapeTag = f }

// SecurityShapeRow and SecurityShapeRows expose the route security claim
// table (T029 SH-S) to the external tests.
type SecurityShapeRow = securityShapeRow

var SecurityShapeRows = securityShapeRows

// SetUnknownPrefixTag installs the unknown-prefix claim hook of the
// Express, FastAPI and Axum projection (T033 UP-A2) for the external
// tests.
func SetUnknownPrefixTag(f func(row string)) { unknownPrefixTag = f }

// UnknownPrefixRow, UnknownPrefixRows and UnknownPrefixWebDefaultForms
// expose the unknown-prefix route contract to the external tests.
type UnknownPrefixRow = upRow

var UnknownPrefixRows = unknownPrefixContract

var UnknownPrefixWebDefaultForms = upWebDefaultForms
