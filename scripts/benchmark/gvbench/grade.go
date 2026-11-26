package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// atomKind says how the values of one answer key are normalised and matched.
// The kind is read off the example value in the question's answer_schema, so
// the question file alone decides grading.
type atomKind string

const (
	kindHandler   atomKind = "handler"   // scalar "file:line", accepted within a line range
	kindRoute     atomKind = "route"     // "METHOD /path"
	kindLocation  atomKind = "location"  // "file:line", line tolerance
	kindPair      atomKind = "pair"      // "METHOD /path @ file:line"
	kindColumn    atomKind = "column"    // "table.column"
	kindFK        atomKind = "fk"        // "table.column -> table.column ON DELETE ACTION"
	kindCondition atomKind = "condition" // "SETTING == value"
)

// lineTolerance is how far a call-site line may be from the verified line.
const lineTolerance = 2

var schemaKinds = map[string]atomKind{
	"METHOD /path":             kindRoute,
	"file:line":                kindLocation,
	"METHOD /path @ file:line": kindPair,
	"table.column":             kindColumn,
	"table.column -> table.column ON DELETE ACTION": kindFK,
	"SETTING == value": kindCondition,
}

func atomKindOf(raw json.RawMessage) (atomKind, error) {
	var scalar string
	if err := json.Unmarshal(raw, &scalar); err == nil {
		if scalar == "file:line" {
			return kindHandler, nil
		}
		return "", fmt.Errorf("unknown scalar schema %q", scalar)
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil || len(list) != 1 {
		return "", fmt.Errorf("schema must be \"file:line\" or a one-element list of an atom shape")
	}
	k, ok := schemaKinds[list[0]]
	if !ok {
		return "", fmt.Errorf("unknown atom shape %q", list[0])
	}
	return k, nil
}

// KeyScore is the grade of one answer key.
type KeyScore struct {
	Kind      atomKind `json:"kind"`
	F1        float64  `json:"f1"`
	Precision float64  `json:"precision"`
	Recall    float64  `json:"recall"`
	Matched   int      `json:"matched"`
	Predicted int      `json:"predicted"`
	Expected  int      `json:"expected"`
	Problem   string   `json:"problem,omitempty"`
}

// Grade is the deterministic score of one answer. Correct needs F1 = 1 on
// every key; Score is the mean key F1, reported alongside.
type Grade struct {
	Answered bool                `json:"answered"`
	Problem  string              `json:"problem,omitempty"`
	Correct  bool                `json:"correct"`
	Score    float64             `json:"score"`
	Keys     map[string]KeyScore `json:"keys,omitempty"`
}

var fenceRE = regexp.MustCompile("(?s)```[jJ][sS][oO][nN][ \t]*\r?\n(.*?)```")

// extractAnswer returns the last fenced json block of the final message as an
// object, or a problem code: no_json_block, invalid_json, not_object.
func extractAnswer(final string) (map[string]json.RawMessage, string, string) {
	matches := fenceRE.FindAllStringSubmatch(final, -1)
	if len(matches) == 0 {
		return nil, "", "no_json_block"
	}
	block := strings.TrimSpace(matches[len(matches)-1][1])
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(block), &obj); err != nil {
		var anyv any
		if json.Unmarshal([]byte(block), &anyv) == nil {
			return nil, block, "not_object"
		}
		return nil, block, "invalid_json"
	}
	if obj == nil {
		return nil, block, "not_object"
	}
	return obj, block, ""
}

// gradeAnswer grades the final message of a run against q. workspace is the
// absolute directory the agent ran in; a path that starts with it is made
// relative before matching, since agents sometimes echo absolute paths.
func gradeAnswer(q Question, final, workspace string) Grade {
	obj, _, problem := extractAnswer(final)
	if problem != "" {
		return Grade{Problem: problem}
	}
	g := Grade{Answered: true, Correct: true, Keys: map[string]KeyScore{}}
	keys := sortedKeys(q.AnswerSchema)
	total := 0.0
	for _, key := range keys {
		kind, _ := atomKindOf(q.AnswerSchema[key])
		ks := gradeKey(q, key, kind, obj[key], workspace)
		g.Keys[key] = ks
		total += ks.F1
		if ks.F1 < 1 {
			g.Correct = false
		}
	}
	g.Score = round4(total / float64(len(keys)))
	return g
}

func gradeKey(q Question, key string, kind atomKind, got json.RawMessage, workspace string) KeyScore {
	ks := KeyScore{Kind: kind}
	if got == nil {
		ks.Problem = "missing_key"
		ks.Expected = expectedCount(q.Expected[key])
		return ks
	}
	if kind == kindHandler {
		return gradeHandler(q, key, got, workspace)
	}
	var want []string
	if err := json.Unmarshal(q.Expected[key], &want); err != nil {
		ks.Problem = "bad_expected"
		return ks
	}
	var have []string
	if err := json.Unmarshal(got, &have); err != nil {
		ks.Problem = "not_a_string_list"
		ks.Expected = len(want)
		return ks
	}
	matched := 0
	switch kind {
	case kindLocation:
		matched = matchLocations(parseLocs(want, ""), parseLocs(have, workspace))
	case kindPair:
		matched = matchPairs(parsePairs(want, ""), parsePairs(have, workspace))
	default:
		matched = matchExact(normAll(kind, want), normAll(kind, have))
	}
	return scoreSet(kind, matched, len(have), len(want))
}

func expectedCount(raw json.RawMessage) int {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return len(list)
	}
	return 1
}

func scoreSet(kind atomKind, matched, predicted, expected int) KeyScore {
	ks := KeyScore{Kind: kind, Matched: matched, Predicted: predicted, Expected: expected}
	if predicted == 0 && expected == 0 {
		ks.F1, ks.Precision, ks.Recall = 1, 1, 1
		return ks
	}
	if predicted > 0 {
		ks.Precision = round4(float64(matched) / float64(predicted))
	}
	if expected > 0 {
		ks.Recall = round4(float64(matched) / float64(expected))
	}
	ks.F1 = round4(2 * float64(matched) / float64(predicted+expected))
	return ks
}

func round4(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 4, 64), 64)
	return v
}

func gradeHandler(q Question, key string, got json.RawMessage, workspace string) KeyScore {
	ks := KeyScore{Kind: kindHandler, Expected: 1}
	var s string
	if err := json.Unmarshal(got, &s); err != nil {
		ks.Problem = "not_a_string"
		return ks
	}
	ks.Predicted = 1
	loc, ok := parseLoc(s, workspace)
	if !ok {
		ks.Problem = "unparsable_location"
		return ks
	}
	accept, ok := q.Accept[key]
	if !ok {
		// Without a range the verified location itself is the only answer.
		var want string
		_ = json.Unmarshal(q.Expected[key], &want)
		w, _ := parseLoc(want, "")
		accept = AcceptRange{File: w.file, LineMin: w.line, LineMax: w.line}
	}
	if loc.file == normPath(accept.File, "") && loc.line >= accept.LineMin && loc.line <= accept.LineMax {
		ks.Matched, ks.F1, ks.Precision, ks.Recall = 1, 1, 1, 1
	}
	return ks
}

type location struct {
	file string
	line int
}

var spaceRE = regexp.MustCompile(`\s+`)

func collapse(s string) string {
	return strings.TrimSpace(spaceRE.ReplaceAllString(s, " "))
}

// trimQuotes removes quotes and backticks agents put around an atom.
func trimQuotes(s string) string {
	return strings.Trim(strings.TrimSpace(s), "`'\"")
}

// normPath makes a path slash-separated and relative: backslashes become
// slashes, a leading workspace prefix and "./" are removed.
func normPath(p, workspace string) string {
	p = strings.ReplaceAll(trimQuotes(p), `\`, "/")
	if workspace != "" {
		ws := strings.TrimSuffix(strings.ReplaceAll(workspace, `\`, "/"), "/") + "/"
		if len(p) >= len(ws) && strings.EqualFold(p[:len(ws)], ws) {
			p = p[len(ws):]
		}
	}
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	return p
}

func parseLoc(s, workspace string) (location, bool) {
	s = trimQuotes(s)
	i := strings.LastIndex(s, ":")
	if i <= 0 {
		return location{}, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(s[i+1:]))
	if err != nil || n <= 0 {
		return location{}, false
	}
	return location{file: normPath(s[:i], workspace), line: n}, true
}

// parseLocs keeps unparsable atoms as a location with line 0, so they still
// count as predicted atoms (lowering precision) but never match.
func parseLocs(items []string, workspace string) []location {
	out := make([]location, 0, len(items))
	for _, it := range items {
		loc, ok := parseLoc(it, workspace)
		if !ok {
			loc = location{file: "\x00unparsable", line: 0}
		}
		out = append(out, loc)
	}
	return out
}

// matchLocations counts a maximum one-to-one matching where two locations
// match when they name the same file and their lines differ by at most
// lineTolerance. Per file, a greedy pass over both sorted line lists is
// optimal for this interval rule.
func matchLocations(want, have []location) int {
	byFileWant := map[string][]int{}
	byFileHave := map[string][]int{}
	for _, l := range want {
		byFileWant[l.file] = append(byFileWant[l.file], l.line)
	}
	for _, l := range have {
		if l.line > 0 {
			byFileHave[l.file] = append(byFileHave[l.file], l.line)
		}
	}
	matched := 0
	for file, w := range byFileWant {
		matched += matchLines(w, byFileHave[file])
	}
	return matched
}

func matchLines(want, have []int) int {
	sort.Ints(want)
	sort.Ints(have)
	i, j, n := 0, 0, 0
	for i < len(want) && j < len(have) {
		d := want[i] - have[j]
		switch {
		case d > lineTolerance:
			j++
		case d < -lineTolerance:
			i++
		default:
			n++
			i++
			j++
		}
	}
	return n
}

type pair struct {
	route string
	loc   location
}

var pairSepRE = regexp.MustCompile(`\s+@\s+`)

func parsePairs(items []string, workspace string) []pair {
	out := make([]pair, 0, len(items))
	for _, it := range items {
		parts := pairSepRE.Split(trimQuotes(it), 2)
		if len(parts) != 2 {
			out = append(out, pair{route: "\x00unparsable"})
			continue
		}
		loc, ok := parseLoc(parts[1], workspace)
		if !ok {
			loc = location{file: "\x00unparsable"}
		}
		out = append(out, pair{route: normRoute(parts[0]), loc: loc})
	}
	return out
}

// matchPairs needs the route part equal after normalisation and the location
// within the line tolerance; grouping by route and file reduces it to
// matchLines.
func matchPairs(want, have []pair) int {
	groupW := map[string][]int{}
	groupH := map[string][]int{}
	for _, p := range want {
		k := p.route + "\x00" + p.loc.file
		groupW[k] = append(groupW[k], p.loc.line)
	}
	for _, p := range have {
		if p.loc.line > 0 {
			k := p.route + "\x00" + p.loc.file
			groupH[k] = append(groupH[k], p.loc.line)
		}
	}
	n := 0
	for k, w := range groupW {
		n += matchLines(w, groupH[k])
	}
	return n
}

var paramRE = regexp.MustCompile(`\{[^}]*\}`)

// normRoute upper-cases the method, collapses whitespace and compares path
// parameters by position only; the trailing slash stays significant.
func normRoute(s string) string {
	s = collapse(trimQuotes(s))
	method, path, ok := strings.Cut(s, " ")
	if !ok {
		return s
	}
	path = strings.ReplaceAll(path, " ", "")
	return strings.ToUpper(method) + " " + paramRE.ReplaceAllString(path, "{}")
}

var fkRE = regexp.MustCompile(`(?i)^(\S+)\s*->\s*(\S+)\s+ON\s+DELETE\s+(.+)$`)

func normAtom(kind atomKind, s string) string {
	switch kind {
	case kindRoute:
		return normRoute(s)
	case kindColumn:
		return strings.ToLower(strings.ReplaceAll(collapse(trimQuotes(s)), " ", ""))
	case kindFK:
		s = collapse(trimQuotes(s))
		if m := fkRE.FindStringSubmatch(s); m != nil {
			return strings.ToLower(m[1]) + " -> " + strings.ToLower(m[2]) + " ON DELETE " + strings.ToUpper(collapse(m[3]))
		}
		return strings.ToLower(s)
	case kindCondition:
		s = strings.NewReplacer(`"`, "", "'", "", "`", "").Replace(s)
		if a, b, ok := strings.Cut(s, "=="); ok {
			return collapse(a) + " == " + collapse(b)
		}
		return collapse(s)
	}
	return collapse(s)
}

func normAll(kind atomKind, items []string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = normAtom(kind, it)
	}
	return out
}

// matchExact counts the multiset intersection, so a duplicated atom in an
// answer counts once as matched and once as a false positive.
func matchExact(want, have []string) int {
	left := map[string]int{}
	for _, w := range want {
		left[w]++
	}
	n := 0
	for _, h := range have {
		if left[h] > 0 {
			left[h]--
			n++
		}
	}
	return n
}
