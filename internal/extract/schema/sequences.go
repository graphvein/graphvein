package schema

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Sequence options (T018a2 Revision 1.5 U5, class P4, rows R1 and N1).
// CREATE SEQUENCE and ALTER SEQUENCE are applied only when every option is
// written once, is one the grammar's SeqOptElem lists, has a signed integer
// constant in int8 range, and the resulting parameters pass
// sequence.c init_params (REL_17_6 1257-1660) computed exactly; OWNED BY
// names a column process_owned_by accepts. Every certain violation is a
// refusal; anything the pass cannot compute may fail.

// seqOption is one SeqOptElem: name is init_params' defname ("as",
// "increment", "start", "restart", "maxvalue", "minvalue", "cache",
// "cycle", "owned_by", "sequence_name", "logged", "unlogged").
type seqOption struct {
	name string
	// value is the integer argument; null marks NO MINVALUE / NO MAXVALUE
	// and RESTART without a value; bad says why the argument is not a
	// signed integer constant in int8 range.
	value int64
	null  bool
	bad   string
	// typ is AS's normalised type ("" when not proven); cycle is CYCLE.
	typ   string
	cycle bool
	// owner is OWNED BY's name parts (["none"] for NONE).
	owner []string
}

// seqOptionWords are the first words of gram.y SeqOptElem: ALTER
// SEQUENCE n followed by one of them is an option list.
var seqOptionWords = map[string]bool{
	"as": true, "cache": true, "cycle": true, "no": true, "increment": true, "maxvalue": true, "minvalue": true,
	"start": true, "restart": true, "owned": true, "sequence": true, "logged": true, "unlogged": true,
}

// sequenceStmt is a CREATE or ALTER SEQUENCE with its option list.
type sequenceStmt struct {
	q       qname
	create  bool
	options []seqOption
	// unread says why the options could not be read (may fail).
	unread string
}

// seqOptionList reads SeqOptList from c to the end of the statement. It
// returns a parse error for text gram.y rejects and for the forms the
// contract sends to D (SEQUENCE NAME and LOGGED/UNLOGGED in CREATE
// SEQUENCE, OWNED BY with one name other than NONE).
func seqOptionList(c *cursor, create bool) (*sequenceStmt, error) {
	st := &sequenceStmt{create: create}
	number := func() (int64, string) {
		sign := ""
		if !c.done() && c.tok().kind == tokOp && (c.tok().value == "-" || c.tok().value == "+") {
			sign = c.tok().value
			c.i++
		}
		if c.done() || c.tok().kind != tokNumber {
			return 0, "expected a number"
		}
		text := c.tok().value
		c.i++
		if !integerText.MatchString(text) {
			return 0, fmt.Sprintf("%s%s is not an integer", sign, text)
		}
		v, err := strconv.ParseInt(strings.TrimPrefix(sign, "+")+text, 10, 64)
		if err != nil {
			return 0, fmt.Sprintf("%s%s is outside the bigint range", sign, text)
		}
		return v, ""
	}
	numeric := func(name string) (seqOption, error) {
		o := seqOption{name: name}
		v, bad := number()
		if bad == "expected a number" {
			return o, c.fail("expected a number after %s", strings.ToUpper(name))
		}
		o.value, o.bad = v, bad
		return o, nil
	}
	for !c.done() {
		var o seqOption
		var err error
		switch {
		case c.acceptKw("as"):
			start := c.i
			if columnType(c) != nil {
				return nil, c.fail("expected a type after AS")
			}
			o = seqOption{name: "as"}
			if base, ok := normalType(c.text(start, c.i)); ok {
				o.typ = base
			}
		case c.acceptKw("cache"):
			o, err = numeric("cache")
		case c.acceptKw("no", "cycle"):
			o = seqOption{name: "cycle"}
		case c.acceptKw("cycle"):
			o = seqOption{name: "cycle", cycle: true}
		case c.acceptKw("increment"):
			c.acceptKw("by")
			o, err = numeric("increment")
		case c.acceptKw("maxvalue"):
			o, err = numeric("maxvalue")
		case c.acceptKw("minvalue"):
			o, err = numeric("minvalue")
		case c.acceptKw("no", "maxvalue"):
			o = seqOption{name: "maxvalue", null: true}
		case c.acceptKw("no", "minvalue"):
			o = seqOption{name: "minvalue", null: true}
		case c.acceptKw("start"):
			c.acceptKw("with")
			o, err = numeric("start")
		case c.acceptKw("restart"):
			with := c.acceptKw("with")
			if !with && (c.done() || c.tok().kind != tokNumber && c.tok().kind != tokOp) {
				o = seqOption{name: "restart", null: true}
			} else {
				o, err = numeric("restart")
			}
		case c.acceptKw("owned", "by"):
			o = seqOption{name: "owned_by"}
			for {
				n, e := c.ident()
				if e != nil {
					return nil, e
				}
				o.owner = append(o.owner, n)
				if !c.acceptPunct(".") {
					break
				}
			}
			if len(o.owner) == 1 && o.owner[0] != "none" {
				// process_owned_by: "invalid OWNED BY option" (42601).
				return nil, c.fail("OWNED BY needs table.column or NONE")
			}
		case c.acceptKw("sequence", "name"):
			if create {
				// init_params: "invalid sequence option SEQUENCE NAME".
				return nil, c.fail("SEQUENCE NAME is not a CREATE SEQUENCE option")
			}
			if _, e := c.name(); e != nil {
				return nil, e
			}
			o = seqOption{name: "sequence_name"}
		case c.acceptKw("logged"), c.acceptKw("unlogged"):
			if create {
				// init_params: option "logged" not recognized.
				return nil, c.fail("LOGGED/UNLOGGED is not a CREATE SEQUENCE option")
			}
			o = seqOption{name: c.toks[c.i-1].value}
		default:
			return nil, c.fail("unexpected sequence option")
		}
		if err != nil {
			return nil, err
		}
		st.options = append(st.options, o)
	}
	return st, nil
}

// seqParams are a sequence's parameters as pg_sequence holds them, and its
// last value.
type seqParams struct {
	typ                 string
	increment, min, max int64
	start, last, cache  int64
	cycle               bool
	// lastKnown is set while no statement may have advanced or set the
	// sequence since its last value was computed.
	lastKnown bool
	// identity marks an identity column's sequence (OWNED BY is refused).
	identity bool
	// opaqueAt is the opaque count when the parameters were computed: an
	// opaque statement since then may have changed them.
	opaqueAt int
}

// typeBounds are the bounds of the sequence data types.
var typeBounds = map[string][2]int64{
	"int2": {math.MinInt16, math.MaxInt16},
	"int4": {math.MinInt32, math.MaxInt32},
	"int8": {math.MinInt64, math.MaxInt64},
}

// initParams computes init_params for opts over cur (isInit for CREATE
// SEQUENCE; cur nil for an ALTER SEQUENCE whose parameters are unknown). It returns the new parameters, or a refusal text, or
// a may-fail text.
func initParams(cur *seqParams, isInit bool, opts []seqOption) (next *seqParams, refused, unknown string) {
	seen := map[string]*seqOption{}
	for i := range opts {
		o := &opts[i]
		if seen[o.name] != nil {
			return nil, fmt.Sprintf("option %s is written twice (42601)", strings.ToUpper(strings.ReplaceAll(o.name, "_", " "))), ""
		}
		seen[o.name] = o
	}
	for _, o := range opts {
		switch {
		case o.bad != "":
			unknown = fmt.Sprintf("%s: %s", strings.ToUpper(o.name), o.bad)
		case o.name == "sequence_name", o.name == "logged", o.name == "unlogged":
			unknown = fmt.Sprintf("%s in the option list is not proven", strings.ToUpper(strings.ReplaceAll(o.name, "_", " ")))
		}
	}
	// Checks that do not depend on the current parameters come first, so
	// that a sequence whose parameters the pass does not know still meets
	// them.
	if o := seen["as"]; o != nil {
		if _, ok := typeBounds[o.typ]; !ok {
			if o.typ == "" {
				if unknown == "" {
					unknown = "the AS type is not proven"
				}
			} else {
				return nil, "sequence type must be smallint, integer, or bigint (22023)", ""
			}
		}
	}
	if o := seen["increment"]; o != nil && o.bad == "" && o.value == 0 {
		return nil, "INCREMENT must not be zero (22023)", ""
	}
	if o := seen["cache"]; o != nil && o.bad == "" && o.value <= 0 {
		return nil, fmt.Sprintf("CACHE (%d) must be greater than zero (22023)", o.value), ""
	}
	if unknown != "" {
		return nil, "", unknown
	}
	if cur == nil && !isInit {
		return nil, "", "the sequence's current parameters are not known"
	}
	p := seqParams{}
	if cur != nil {
		p = *cur
	}
	resetMax, resetMin := false, false
	if o := seen["as"]; o != nil {
		if !isInit {
			b := typeBounds[p.typ]
			resetMax = p.max == b[1]
			resetMin = p.min == b[0]
		}
		p.typ = o.typ
	} else if isInit {
		p.typ = "int8"
	}
	b := typeBounds[p.typ]
	if o := seen["increment"]; o != nil {
		p.increment = o.value
	} else if isInit {
		p.increment = 1
	}
	if o := seen["cycle"]; o != nil {
		p.cycle = o.cycle
	}
	if o := seen["maxvalue"]; o != nil && !o.null {
		p.max = o.value
	} else if isInit || o != nil || resetMax {
		if p.increment > 0 || resetMax {
			p.max = b[1]
		} else {
			p.max = -1
		}
	}
	if p.max < b[0] || p.max > b[1] {
		return nil, fmt.Sprintf("MAXVALUE (%d) is out of range for the sequence data type (22023)", p.max), ""
	}
	if o := seen["minvalue"]; o != nil && !o.null {
		p.min = o.value
	} else if isInit || o != nil || resetMin {
		if p.increment < 0 || resetMin {
			p.min = b[0]
		} else {
			p.min = 1
		}
	}
	if p.min < b[0] || p.min > b[1] {
		return nil, fmt.Sprintf("MINVALUE (%d) is out of range for the sequence data type (22023)", p.min), ""
	}
	if p.min >= p.max {
		return nil, fmt.Sprintf("MINVALUE (%d) must be less than MAXVALUE (%d) (22023)", p.min, p.max), ""
	}
	if o := seen["start"]; o != nil {
		p.start = o.value
	} else if isInit {
		if p.increment > 0 {
			p.start = p.min
		} else {
			p.start = p.max
		}
	}
	if p.start < p.min {
		return nil, fmt.Sprintf("START value (%d) cannot be less than MINVALUE (%d) (22023)", p.start, p.min), ""
	}
	if p.start > p.max {
		return nil, fmt.Sprintf("START value (%d) cannot be greater than MAXVALUE (%d) (22023)", p.start, p.max), ""
	}
	if o := seen["restart"]; o != nil {
		if o.null {
			p.last = p.start
		} else {
			p.last = o.value
		}
		p.lastKnown = true
	} else if isInit {
		p.last, p.lastKnown = p.start, true
	}
	switch {
	case p.lastKnown && p.last < p.min:
		return nil, fmt.Sprintf("RESTART value (%d) cannot be less than MINVALUE (%d) (22023)", p.last, p.min), ""
	case p.lastKnown && p.last > p.max:
		return nil, fmt.Sprintf("RESTART value (%d) cannot be greater than MAXVALUE (%d) (22023)", p.last, p.max), ""
	case !p.lastKnown && (p.min > cur.min || p.max < cur.max):
		// The last value lies within the old bounds; narrower bounds may
		// exclude it.
		return nil, "", "the bounds narrow and the sequence's last value is not known"
	}
	if o := seen["cache"]; o != nil {
		p.cache = o.value
	} else if isInit {
		p.cache = 1
	}
	return &p, "", ""
}

// serialParams are the parameters Postgres gives the sequence of a serial
// or identity column of base type typ (int2, int4 or int8).
func serialParams(typ string, identity bool) *seqParams {
	b := typeBounds[typ]
	return &seqParams{typ: typ, increment: 1, min: 1, max: b[1], start: 1, last: 1, cache: 1, identity: identity}
}

// sequenceStatement applies U5 to a CREATE or ALTER SEQUENCE before the
// state records it (rows R1 and N1).
func (s *state) sequenceStatement(st *sequenceStmt) {
	if s.sequences == nil {
		s.sequences = map[string]*seqParams{}
	}
	id, verb := "N1.P4", "ALTER SEQUENCE "
	if st.create {
		id, verb = "R1.P4", "CREATE SEQUENCE "
	}
	what := verb + st.q.String()
	key := st.q.String()
	if st.unread != "" {
		s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", what, st.unread, precondition(id)))
		delete(s.sequences, key)
		return
	}
	var cur *seqParams
	if !st.create {
		cur = s.currentSequence(st.q)
	}
	next, refused, unknown := initParams(cur, st.create, st.options)
	switch {
	case refused != "":
		s.mayFail(fmt.Sprintf("%s: %s (Postgres refuses)%s", what, refused, precondition(id)))
		return
	case unknown != "":
		s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", what, unknown, precondition(id)))
		delete(s.sequences, key)
		return
	}
	for _, o := range st.options {
		if o.name == "owned_by" {
			s.ownedBy(st, o.owner, cur, what, id)
		}
	}
	if s.sequenceCalls {
		// nextval or setval in the set may advance or set it.
		next.lastKnown = false
	}
	next.opaqueAt = s.opaqueSeen
	if cur != nil {
		next.identity = cur.identity
	}
	s.sequences[key] = next
}

// currentSequence returns the parameters of the sequence q names when the
// pass knows them: a sequence the set created with options it read and
// changed only by ALTER SEQUENCE statements it read, or the sequence of a
// serial or identity column (Postgres's defaults for the column's type).
func (s *state) currentSequence(q qname) *seqParams {
	if q.schema == nil && s.pathChanged {
		return nil
	}
	if p := s.sequences[q.String()]; p != nil {
		if p.opaqueAt != s.opaqueSeen {
			return nil
		}
		if _, certain, found := s.relationAt(q); found && certain {
			return p
		}
	}
	for _, t := range s.tables {
		if !sameSchema(t.q.schema, q.schema) || !factsKnown(t) || t.mayBeOther || s.opaqueSeen > 0 {
			continue
		}
		for _, c := range t.columns {
			if !(c.implied || c.identity != nil) || seqName(t, c) != q.name {
				continue
			}
			base, _ := normalType(c.typ)
			switch {
			case c.identity != nil:
				if _, ok := typeBounds[base]; ok {
					return serialParams(base, true)
				}
			case base == "smallserial" || base == "serial2":
				return serialParams("int2", false)
			case base == "serial" || base == "serial4":
				return serialParams("int4", false)
			case base == "bigserial" || base == "serial8":
				return serialParams("int8", false)
			}
			return nil
		}
	}
	return nil
}

// ownedBy applies process_owned_by: OWNED BY NONE, or a column of a
// table in the sequence's schema, not on an identity sequence (0A000).
func (s *state) ownedBy(st *sequenceStmt, owner []string, cur *seqParams, what, id string) {
	if cur != nil && cur.identity {
		// process_owned_by checks this for OWNED BY NONE too.
		s.mayFail(fmt.Sprintf("%s OWNED BY: cannot change ownership of identity sequence (Postgres refuses, 0A000)%s", what, precondition(id)))
		return
	}
	if len(owner) == 1 {
		return
	}
	if len(owner) > 3 {
		s.unknownFact(fmt.Sprintf("%s OWNED BY %s may be refused: the name has too many parts%s", what, strings.Join(owner, "."), precondition(id)))
		return
	}
	col := owner[len(owner)-1]
	tq := qname{name: owner[len(owner)-2]}
	if len(owner) == 3 {
		tq.schema = &owner[0]
	}
	unproven := func(why string) {
		s.unknownFact(fmt.Sprintf("%s OWNED BY %s.%s may be refused: %s%s", what, tq, col, why, precondition(id)))
	}
	exact, others := s.lookup(tq)
	if exact == nil || len(others) > 0 || tq.schema == nil && (s.pathChanged || s.temps[tq.name]) || exact.mayBeOther {
		unproven("the pass does not hold the table under a certain name")
		return
	}
	switch {
	case sameSchema(st.q.schema, tq.schema):
	case st.q.schema != nil && tq.schema != nil:
		s.mayFail(fmt.Sprintf("%s OWNED BY %s.%s: sequence must be in same schema as table it is linked to (Postgres refuses, 55000)%s", what, tq, col, precondition(id)))
		return
	default:
		unproven("whether the sequence and the table are in the same schema is not proven")
		return
	}
	if exact.column(col) == nil {
		if complete(exact) {
			s.mayFail(fmt.Sprintf("%s OWNED BY %s.%s: the column does not exist (Postgres refuses, 42703)%s", what, tq, col, precondition(id)))
			return
		}
		unproven("the table is partial, so the column may not exist")
		return
	}
	if !factsKnown(exact) {
		unproven("the table's facts are not known")
	}
}

// sequenceAfter keeps the read parameters in step with the statement's
// effect on names: a dropped or renamed sequence loses them, and an OWNED
// BY makes the sequence's existence unproven (ownedSequence).
func (s *state) sequenceAfter(a analysis) {
	for _, o := range a.dropped {
		delete(s.sequences, o.q.String())
	}
	if a.renamed != nil && a.renamed.newName != "" {
		delete(s.sequences, a.renamed.q.String())
	}
	if a.sequence != nil {
		s.ownedSequence(a.sequence)
	}
}

// ownedSequence marks a sequence an OWNED BY made depend on a column:
// dropping the table or column drops it, which the pass does not follow,
// so its existence is no longer proven after the statement.
func (s *state) ownedSequence(st *sequenceStmt) {
	for _, o := range st.options {
		if o.name == "owned_by" && len(o.owner) > 1 {
			if r := s.relations[st.q.String()]; r != nil {
				r.uncertain = true
			}
			delete(s.sequences, st.q.String())
		}
	}
}
