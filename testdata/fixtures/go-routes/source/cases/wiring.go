package cases

// Calls of router factories in a file the route gate does not pass (no
// router import; row RN-results): their results' uses are read
// from the payload. U1 hands mkU1's router to keep, which no rule reads
// (the router is really served under /api: no path); U2 hands mkU2's to
// serveRoot, which serves it at its own paths (path /u2users, as gotify's
// main serves router.Create's engine through runner.Run).

func keep(h any) any { return prefixed(h) }

// U1 serves mkU1's router through an unmodelled helper.
func U1() any { return keep(mkU1()) }

// U2 serves mkU2's router at its own paths.
func U2() any {
	h := mkU2()
	return serveRoot(h)
}
