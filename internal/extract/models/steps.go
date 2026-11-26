package models

import (
	"fmt"

	"github.com/graphvein/graphvein/internal/contract"
)

// Steps collects a front end's diagnostics and runs its steps in
// isolation: a failure (a panic) in one class, site or table step becomes
// a schema_model_internal_error diagnostic and never stops the others
// (AGENTS.md: one file's failure must not stop the others).
type Steps struct {
	Diagnostics []contract.Diagnostic
	// Hook, when set (tests only), runs at the start of every step with the
	// step name and its subject, so a test can make one step fail.
	Hook func(step, subject string)
	// Panics, when true (tests only), re-raises a step's panic to show its
	// stack.
	Panics bool
}

// Diag appends one diagnostic.
func (s *Steps) Diag(file, severity, code, message string) {
	s.Diagnostics = append(s.Diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: severity, Code: code, Message: message})
}

// Guard runs fn as one isolated step on file.
func (s *Steps) Guard(file, step, subject string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			if s.Panics {
				panic(r)
			}
			s.Diag(file, "warning", "schema_model_internal_error", fmt.Sprintf("%s could not be linked (%s step): %v", subject, step, r))
		}
	}()
	if s.Hook != nil {
		s.Hook(step, subject)
	}
	fn()
}
