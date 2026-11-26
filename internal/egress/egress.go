// Package egress is the only package allowed to open network connections.
//
// Every outbound dial, every DNS lookup by name and every listening socket goes
// through this package, so one allow-list decides what a run may reach. The
// default is offline: only infrastructure the user configured explicitly (the
// team database today) is reachable. Blocked attempts fail closed with an
// error wrapping ErrBlocked. Tests in this package enforce the boundary for the
// rest of the module (see boundary_test.go and deps_test.go).
package egress

import (
	"errors"
	"fmt"
	"sync"
)

// Mode selects which kinds of destinations a run may reach. It is a coarse
// switch the user sets in graphvein.yaml; individual destinations still have
// to be configured.
type Mode string

const (
	// Offline allows only explicitly configured infrastructure such as the
	// team database. It is the default.
	Offline Mode = "offline"
	// LLM additionally allows configured LLM providers. No providers exist
	// yet, so today it permits exactly what Offline permits.
	LLM Mode = "llm"
)

// ParseMode accepts exactly the mode names that configuration accepts, so a
// typo cannot silently fall back to a more permissive mode.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case Offline, LLM:
		return Mode(s), nil
	}
	return "", fmt.Errorf("egress mode must be offline or llm, got %q", s)
}

// Purpose records why a destination is allowed. Modes filter destinations by
// purpose, and audits report it with every attempt.
type Purpose string

const (
	// Database is explicitly configured storage infrastructure (Postgres).
	// It is allowed in every mode.
	Database Purpose = "database"
	// LLMProvider is a configured model provider. It is allowed only in LLM
	// mode.
	LLMProvider Purpose = "llm"
	// TryIt is the origin of a "try it" environment the user configured in
	// graphvein.yaml or the project manifest (T029, ADR 0081). Like
	// Database it is configured infrastructure, allowed in every mode; the
	// server builds one policy per request holding only the chosen
	// environment's origin.
	TryIt Purpose = "try-it"
)

// ErrBlocked is wrapped by every error caused by the allow-list, so callers
// and tests can tell a policy refusal from an ordinary network failure.
var ErrBlocked = errors.New("egress: destination not allowed")

// Event describes one attempted lookup, dial or listen, allowed or not. It
// carries addresses only, never credentials.
type Event struct {
	Op      string // "lookup", "dial" or "listen"
	Network string // "tcp", "unix", ...; empty for lookups
	Address string // host for lookups, host:port or socket path otherwise
	Purpose Purpose
	Allowed bool
}

var observers = struct {
	sync.RWMutex
	next int
	fns  map[int]func(Event)
}{fns: map[int]func(Event){}}

// Observe registers fn to receive every Event in the process until stop is
// called. It exists for the enforcement tests and future audit output;
// without observers reporting costs one lock. Observers are process-wide, so
// tests that use them must not run in parallel with other network tests.
func Observe(fn func(Event)) (stop func()) {
	observers.Lock()
	id := observers.next
	observers.next++
	observers.fns[id] = fn
	observers.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			observers.Lock()
			delete(observers.fns, id)
			observers.Unlock()
		})
	}
}

// report calls observers on a copy of the list, outside the lock, so an
// observer may call its own stop (or Observe) without deadlocking.
func report(e Event) {
	observers.RLock()
	fns := make([]func(Event), 0, len(observers.fns))
	for _, fn := range observers.fns {
		fns = append(fns, fn)
	}
	observers.RUnlock()
	for _, fn := range fns {
		fn(e)
	}
}
