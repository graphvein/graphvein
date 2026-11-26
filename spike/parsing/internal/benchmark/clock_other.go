//go:build !windows

package benchmark

import "time"

const timingMethod = "Go time.Since monotonic clock"

var clockOrigin = time.Now()

func timestamp() (int64, error) { return time.Since(clockOrigin).Nanoseconds(), nil }
