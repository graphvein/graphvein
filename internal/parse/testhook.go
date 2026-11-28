package parse

import (
	"context"
	"time"
)

// HoldParseUntilDeadline makes every parse started after the call wait,
// once its per-file deadline is set, until that deadline has passed, and
// returns a function that removes the hold. It exists for tests in other
// packages that need a parse to time out: a tiny Options.Timeout alone races
// the clock and can lose on a fast machine or one with coarse timers
// (Windows), so the parse finishes before the deadline fires. Parses without
// a timeout are not held. It is test support only: call it, and the returned
// restore function, while no Batch or Consume is running.
func HoldParseUntilDeadline() (restore func()) {
	parseHook = func(ctx context.Context, _ []byte) {
		if _, ok := ctx.Deadline(); ok {
			<-ctx.Done()
		}
	}
	return func() { parseHook = nil }
}

// SimulateParseWork makes every parse started after the call behave as if
// parsing took work: when the file's deadline leaves at least work, the
// parse goes on at once (the work fitted); otherwise it waits until the
// deadline has passed, so it fails as a parse that outlasted its deadline
// would. It lets tests in other packages pin which deadline a file got
// without racing the clock. Test support only, with the same rules as
// HoldParseUntilDeadline.
func SimulateParseWork(work time.Duration) (restore func()) {
	parseHook = func(ctx context.Context, _ []byte) {
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < work {
			<-ctx.Done()
		}
	}
	return func() { parseHook = nil }
}
