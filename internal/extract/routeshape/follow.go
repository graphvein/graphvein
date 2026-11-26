package routeshape

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Depth limits of call following (row SM-21). The limit a run uses comes
// from graphvein.yaml (index.shape_depth) or graphvein index
// --shape-depth; DefaultDepth applies otherwise. Following runs at link
// time over cached syntax, so a new limit needs a re-index (a re-link),
// never a re-parse; an extractor that needs parse-time data caches enough
// for MaxDepthLimit.
const (
	MinDepth      = 1
	DefaultDepth  = 6
	MaxDepthLimit = 10
)

// MaxFrames bounds the functions one Follow call reads (row SM-26), so a
// wide fan-out cannot make linking slow. A call not followed because of
// it ends its chain with StopBudget.
const MaxFrames = 256

// Stop is why a followed chain ended (rows SM-22..SM-28).
type Stop string

const (
	// StopEnded: the value is not passed on from the last function read.
	StopEnded Stop = "ended"
	// StopDepthCap: the next call is proven, but following it would pass
	// the depth limit. Shown as "depth cap reached (N)", never as unknown.
	StopDepthCap Stop = "depth_cap"
	// StopCycle: the next call returns to a function already on the chain
	// with the same value.
	StopCycle Stop = "cycle"
	// StopJoined: the call reaches a function and value already read
	// through another chain; its reads are already recorded.
	StopJoined Stop = "joined"
	// StopBudget: MaxFrames functions were already read.
	StopBudget Stop = "budget"
	// StopResolverError: reading the function failed; recorded, never
	// fatal (one file's failure never stops the others).
	StopResolverError Stop = "resolver_error"
	// StopUnresolved: the call target is not proven (no unique stored
	// definition).
	StopUnresolved Stop = "unresolved"
	// StopAmbiguous: several definitions could be the target.
	StopAmbiguous Stop = "ambiguous"
	// StopExternal: the target is outside the repository (a library).
	StopExternal Stop = "external"
	// StopTransformed: the value is transformed before it is passed on
	// (spread, merge, re-assignment), so the callee does not see it as is.
	StopTransformed Stop = "transformed"
	// StopOpaqueType: the value's type is any, unknown or an
	// uninstantiated generic, so its shape cannot be read further.
	StopOpaqueType Stop = "opaque_type"
)

// Stops lists every stop reason in row order.
var Stops = []Stop{StopEnded, StopDepthCap, StopCycle, StopJoined, StopBudget, StopResolverError, StopUnresolved, StopAmbiguous, StopExternal, StopTransformed, StopOpaqueType}

// Frame is one function being read with the value followed into it.
type Frame struct {
	// Function is the function's definition: file, line and name.
	Function Step
	// Value is the function's own name for the followed value (the
	// parameter that received it); for the start frame, what the
	// extractor follows (the handler's request parameter).
	Value string
	// Depth is the number of calls followed to get here (0 at the start).
	Depth int
	// Chain is the calls followed to get here, outermost first: each
	// step is a call site with the called function's name. An item found
	// in this frame takes it as Provenance.Chain.
	Chain []Step
}

// Call is one place where the followed value leaves a function, as the
// resolver reads it.
type Call struct {
	// Site is the call as written: file, line, and the callee expression.
	Site Step
	// Stop is empty when the target is proven (Target and Value set), or
	// the reason it cannot be followed: StopUnresolved, StopAmbiguous,
	// StopExternal, StopTransformed or StopOpaqueType.
	Stop Stop
	// Target is the proven callee's definition.
	Target Step
	// Value is the callee's name for the value (its parameter).
	Value string
	// Detail is free text for the record (the candidates of an ambiguous
	// call, the type that is opaque).
	Detail string
}

// Resolver is what a framework's shape extractor implements per language.
//
// Visit reads one function (collecting whatever reads of frame.Value the
// extractor wants, with frame.Chain as their provenance chain) and returns
// the calls through which the value leaves it, in source order. Only a
// call whose target is proven (a unique stored definition, never a name
// guess) may have an empty Stop. An error ends this chain with
// StopResolverError and following continues elsewhere.
type Resolver interface {
	Visit(ctx context.Context, frame Frame) ([]Call, error)
}

// ResolverFunc adapts a function to Resolver.
type ResolverFunc func(ctx context.Context, frame Frame) ([]Call, error)

// Visit calls f.
func (f ResolverFunc) Visit(ctx context.Context, frame Frame) ([]Call, error) { return f(ctx, frame) }

// Chain is one followed chain and how it ended.
type Chain struct {
	Start Step
	// Steps are the calls followed; len(Steps) == Depth.
	Steps []Step
	Depth int
	Stop  Stop
	// At is where it stopped (nil for StopEnded and StopResolverError).
	At     *Step
	Detail string
}

// Result is everything one Follow call read.
type Result struct {
	// DepthLimit is the limit the chains were followed with.
	DepthLimit int
	// Frames are the functions read, in visit order.
	Frames []Frame
	// Chains are the chain ends, in visit order.
	Chains []Chain
}

// ValidDepth reports whether depth is an allowed limit (MinDepth to
// MaxDepthLimit).
func ValidDepth(depth int) bool { return depth >= MinDepth && depth <= MaxDepthLimit }

// Follow reads start with r and follows every proven call that carries
// the value, depth first in the order r returns calls, until depthLimit
// calls deep (rows SM-21..SM-28). It records every chain end with its
// depth and stop reason. It never follows an unproven call, never reads a
// function twice for the same value, and never fails because of r: the
// only error is an invalid depth limit.
func Follow(ctx context.Context, start Frame, depthLimit int, r Resolver) (Result, error) {
	if !ValidDepth(depthLimit) {
		return Result{}, fmt.Errorf("shape depth %d is outside %d..%d", depthLimit, MinDepth, MaxDepthLimit)
	}
	f := &follower{r: r, limit: depthLimit, start: start.Function, visited: map[string]bool{}, onPath: map[string]bool{}}
	f.res.DepthLimit = depthLimit
	start.Depth, start.Chain = 0, nil
	f.visited[frameKey(start.Function, start.Value)] = true
	f.visit(ctx, start)
	return f.res, nil
}

type follower struct {
	r       Resolver
	limit   int
	start   Step
	res     Result
	visited map[string]bool
	onPath  map[string]bool
}

func frameKey(fn Step, value string) string {
	return fn.File + "\x00" + strconv.Itoa(fn.Line) + "\x00" + fn.Function + "\x00" + value
}

func (f *follower) end(fr Frame, stop Stop, at *Step, detail string) {
	f.res.Chains = append(f.res.Chains, Chain{Start: f.start, Steps: copySteps(fr.Chain), Depth: fr.Depth, Stop: stop, At: at, Detail: detail})
}

func copySteps(s []Step) []Step {
	out := make([]Step, len(s))
	copy(out, s)
	return out
}

func (f *follower) visit(ctx context.Context, fr Frame) {
	key := frameKey(fr.Function, fr.Value)
	f.onPath[key] = true
	defer delete(f.onPath, key)
	f.res.Frames = append(f.res.Frames, Frame{Function: fr.Function, Value: fr.Value, Depth: fr.Depth, Chain: copySteps(fr.Chain)})
	calls, err := f.r.Visit(ctx, fr)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		f.end(fr, StopResolverError, nil, err.Error())
		return
	}
	if len(calls) == 0 {
		f.end(fr, StopEnded, nil, "")
		return
	}
	for _, c := range calls {
		site := c.Site
		switch c.Stop {
		case "":
		case StopUnresolved, StopAmbiguous, StopExternal, StopTransformed, StopOpaqueType:
			f.end(fr, c.Stop, &site, c.Detail)
			continue
		default:
			f.end(fr, StopUnresolved, &site, fmt.Sprintf("resolver returned stop %q", c.Stop))
			continue
		}
		if !validStep(c.Target) || c.Value == "" {
			f.end(fr, StopUnresolved, &site, "resolver returned a proven call without a target")
			continue
		}
		next := frameKey(c.Target, c.Value)
		step := Step{File: site.File, Line: site.Line, Function: c.Target.Function}
		switch {
		case f.onPath[next]:
			f.end(fr, StopCycle, &step, "")
		case fr.Depth+1 > f.limit:
			f.end(fr, StopDepthCap, &step, fmt.Sprintf("depth cap reached (%d)", f.limit))
		case f.visited[next]:
			f.end(fr, StopJoined, &step, "")
		case len(f.res.Frames) >= MaxFrames:
			f.end(fr, StopBudget, &step, fmt.Sprintf("%d functions already read", MaxFrames))
		default:
			f.visited[next] = true
			chain := append(copySteps(fr.Chain), step)
			f.visit(ctx, Frame{Function: c.Target, Value: c.Value, Depth: fr.Depth + 1, Chain: chain})
		}
	}
}

// Records converts a Result's chains into the records stored on the
// route (Shape.Follow.Chains) for one section.
func Records(section string, r Result) []ChainRecord {
	out := make([]ChainRecord, 0, len(r.Chains))
	for _, c := range r.Chains {
		var at *Step
		if c.At != nil {
			a := *c.At
			at = &a
		}
		out = append(out, ChainRecord{Section: section, Start: c.Start, Steps: copySteps(c.Steps), Depth: c.Depth, Stop: c.Stop, At: at, Detail: c.Detail})
	}
	return out
}

// Stats is a histogram of chain depths and stop reasons (row SM-29). The
// shape extractors report it per corpus, so the default depth limit is
// set from measured data: how many chains hit the cap, and how deep
// chains that ended on their own went.
type Stats struct {
	Chains int
	// Depth counts chains by the depth they reached.
	Depth map[int]int
	// Stop counts chains by stop reason.
	Stop map[Stop]int
	// Limits counts chains by the depth limit they were followed with.
	Limits map[int]int
}

// NewStats returns empty statistics.
func NewStats() *Stats {
	return &Stats{Depth: map[int]int{}, Stop: map[Stop]int{}, Limits: map[int]int{}}
}

// Add counts the chains of one Follow call.
func (s *Stats) Add(r Result) {
	for _, c := range r.Chains {
		s.count(c.Depth, c.Stop, r.DepthLimit)
	}
}

// AddShape counts the chains stored on one route.
func (s *Stats) AddShape(sh *Shape) {
	for _, c := range sh.Follow.Chains {
		s.count(c.Depth, c.Stop, sh.Follow.DepthLimit)
	}
}

func (s *Stats) count(depth int, stop Stop, limit int) {
	s.Chains++
	s.Depth[depth]++
	s.Stop[stop]++
	s.Limits[limit]++
}

// AtCap is the number of chains cut by the depth limit.
func (s *Stats) AtCap() int { return s.Stop[StopDepthCap] }

// Table renders the statistics as a deterministic text table for corpus
// test logs and task reports.
func (s *Stats) Table() string {
	var b strings.Builder
	fmt.Fprintf(&b, "chains %d, at depth cap %d\n", s.Chains, s.AtCap())
	limits := make([]int, 0, len(s.Limits))
	for l := range s.Limits {
		limits = append(limits, l)
	}
	sort.Ints(limits)
	for _, l := range limits {
		fmt.Fprintf(&b, "limit %d: %d\n", l, s.Limits[l])
	}
	depths := make([]int, 0, len(s.Depth))
	for d := range s.Depth {
		depths = append(depths, d)
	}
	sort.Ints(depths)
	for _, d := range depths {
		fmt.Fprintf(&b, "depth %d: %d\n", d, s.Depth[d])
	}
	for _, st := range Stops {
		if n := s.Stop[st]; n > 0 {
			fmt.Fprintf(&b, "stop %s: %d\n", st, n)
		}
	}
	return b.String()
}
