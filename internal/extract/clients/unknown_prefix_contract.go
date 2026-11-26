package clients

// UnknownPrefixMatcherRows are the client matcher rows of the
// unknown-prefix path claim contract (task T033, ADR 0084;
// docs/contract/claims/unknown-prefix-paths.md, section "Client matcher",
// copied in docs/extract/clients.md) in table order, then the single
// default branch D. Every branch of the tier (linkUnknownPrefix) reports
// one of them through upTag; TestUnknownPrefixMatcherContract keeps this
// list, the public table and the docs copy 1:1, and the unknown-prefix
// fixture (internal/index TestUnknownPrefixLinks) reaches every row.
var UnknownPrefixMatcherRows = []string{"UP-13", "UP-14", "UP-15", "UP-16", "UP-17", "UP-18", "UP-19", "UP-20", "UP-21", "UP-22", "D"}

// UnknownPrefixCallRows are the call-side rows of the same contract
// (section "Call side") in table order, then the default branch
// D. Every branch of runtimeBase (and the auto-import binding of the
// helper call, row UP-41) reports one of them through upCallTag;
// TestUnknownPrefixCallContract keeps this list, the public table and the
// docs copy 1:1, and the unknown-prefix-helpers fixture
// (internal/index TestUnknownPrefixHelpers) reaches every row.
var UnknownPrefixCallRows = []string{"UP-39", "UP-40", "UP-41", "D"}
