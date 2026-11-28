package parse

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// TestConsumeRetriesTimeoutsSerially pins T074: a file whose parse runs past
// its deadline in the parallel pass is parsed again once, after every
// parallel parse has finished, alone and with RetryTimeout; its result is
// the tree an untimed parse gives. Other failures are not retried. The hook
// simulates parsing work that fits RetryTimeout but not the base deadline,
// so the test does not race the clock.
func TestConsumeRetriesTimeoutsSerially(t *testing.T) {
	const base, work = 40 * time.Millisecond, 200 * time.Millisecond
	if RetryTimeout(base) < 2*work {
		t.Fatal("retry deadline does not fit the simulated work")
	}
	slow := map[string]bool{"function slowA() { return 1; }": true, "function slowB() { return 2; }": true}
	jobs := []Job{
		{"javascript", []byte("function slowA() { return 1; }")},
		{"javascript", []byte("function quick() {}")},
		{"unavailable", []byte("not a grammar")},
		{"javascript", []byte("function slowB() { return 2; }")},
		{"typescript", []byte("export const x = 1;")},
	}
	// active counts running parses; a retry must find itself alone.
	var active, retriesNotAlone atomic.Int32
	parseHook = func(ctx context.Context, source []byte) {
		n := active.Add(1)
		defer active.Add(-1)
		deadline, _ := ctx.Deadline()
		if time.Until(deadline) > base {
			// The retry's deadline: only the slow files get one.
			if n != 1 {
				retriesNotAlone.Add(1)
			}
			return
		}
		if slow[string(source)] {
			// The work does not fit the base deadline.
			<-ctx.Done()
		}
	}
	t.Cleanup(func() { parseHook = nil })
	options := Options{Workers: 4, CacheDir: filepath.Join(t.TempDir(), "cache"), Timeout: base}
	results := map[int]Result{}
	var consumed []int
	err := Consume(context.Background(), jobs, options, func(i int, r Result) {
		results[i] = r
		consumed = append(consumed, i)
	})
	parseHook = nil
	if err != nil {
		t.Fatal(err)
	}
	if len(consumed) != len(jobs) {
		t.Fatalf("consumed %v, want every job once", consumed)
	}
	// The retries come last, in input order.
	if consumed[3] != 0 || consumed[4] != 3 {
		t.Fatalf("consumed %v, want the retried 0 and 3 last", consumed)
	}
	for _, i := range []int{0, 3} {
		r := results[i]
		if r.Err != nil || r.Root == nil || !r.Retried {
			t.Fatalf("job %d: %+v, want a retried success", i, r)
		}
	}
	if r := results[2]; r.Err == nil || r.Retried || IsTimeout(r.Err) {
		t.Fatalf("missing grammar: %+v, want a failure that is not retried", r)
	}
	for _, i := range []int{1, 4} {
		if r := results[i]; r.Err != nil || r.Retried {
			t.Fatalf("job %d: %+v", i, r)
		}
	}
	if retriesNotAlone.Load() != 0 {
		t.Fatal("a retry ran while another parse was running")
	}
	// The retried tree is the tree of a parse without any deadline.
	want, err := Batch(context.Background(), []Job{jobs[0], jobs[3]}, Options{Workers: 1, CacheDir: options.CacheDir})
	if err != nil {
		t.Fatal(err)
	}
	for k, i := range []int{0, 3} {
		if !reflect.DeepEqual(results[i].Root, want[k].Root) {
			t.Fatalf("job %d: retried tree differs from an untimed parse", i)
		}
	}
}

// TestConsumeRetryTimesOutAgain pins that a file whose retry also runs past
// its deadline is consumed once, with that timeout, marked as retried, and
// that the retry had RetryTimeout.
func TestConsumeRetryTimesOutAgain(t *testing.T) {
	restore := HoldParseUntilDeadline()
	t.Cleanup(restore)
	const base = 10 * time.Millisecond
	var got []Result
	err := Consume(context.Background(), []Job{{"javascript", []byte("function f() {}")}}, Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache"), Timeout: base}, func(_ int, r Result) {
		got = append(got, r)
	})
	restore()
	if err != nil || len(got) != 1 {
		t.Fatal(err, got)
	}
	var timeout *TimeoutError
	if !got[0].Retried || !errors.As(got[0].Err, &timeout) || timeout.Deadline != RetryTimeout(base) || !errors.Is(got[0].Err, context.DeadlineExceeded) {
		t.Fatalf("%+v, want a retried timeout with the retry deadline", got[0])
	}
}
