package clients

// SetAuthTag installs the DA-H claim-branch hook for the external tests,
// which index the fixture through internal/index (an import cycle for an
// in-package test).
func SetAuthTag(f func(row string)) { authTag = f }
