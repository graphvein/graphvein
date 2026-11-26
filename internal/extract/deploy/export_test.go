package deploy

// SetClaimTag installs the claim-branch hook for the external tests, which
// index the fixture through internal/index (an import cycle for an
// in-package test).
func SetClaimTag(f func(row string)) { claimTag = f }
