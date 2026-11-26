package tableaccess

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file holds rows CT-S1 (the checked query macros) and CT-S2 (the
// query functions) over the cached sqlx payload (docs/lang/rust-sqlx.md).

// sqlxFile is one Rust file's sqlx sites in link order.
type sqlxFile struct {
	in        Input
	file      string
	facts     facts.Facts
	byPackage map[string][]*migrationSet
}

// linkSqlxFile reads the file's query sites and returns the spans of those
// that name sqlx (for the execution rule).
func linkSqlxFile(in Input, file string, f facts.Facts, byPackage map[string][]*migrationSet, a *analysis) []contract.Span {
	l := sqlxFile{in: in, file: file, facts: f, byPackage: byPackage}
	var spans []contract.Span
	for _, q := range f.Rust.Queries {
		spelling, ok := l.sqlxSpelling(q)
		if !ok {
			// Not proven to be sqlx's: no site.
			continue
		}
		spans = append(spans, q.Span)
		var s site
		if q.Macro {
			s = l.claimSqlxMacro(q, spelling)
		} else {
			s = l.claimSqlxFunction(q)
		}
		a.sites = append(a.sites, s)
	}
	return spans
}

// sqlxSpelling reports how a site's path names sqlx: "sqlx::<m>!" for a
// two-segment sqlx path, "use sqlx::" for one segment a use declaration in
// the site's module imports from sqlx. Any other path is not a site.
func (l sqlxFile) sqlxSpelling(q facts.RustQuery) (string, bool) {
	switch {
	case len(q.Path) == 2 && q.Path[0] == "sqlx":
		return "sqlx::<m>!", true
	case len(q.Path) == 1:
		scopes := l.facts.Rust.Scopes
		if q.Scope < 0 || q.Scope >= len(scopes) {
			return "", false
		}
		// A use declaration reaches the blocks of its module, never a
		// nested module: walk parents while the module stays the same.
		module := scopes[q.Scope].Module
		for s := q.Scope; s >= 0 && s < len(scopes) && scopes[s].Module == module; {
			for _, imp := range scopes[s].Imports {
				if strings.TrimPrefix(imp.Bindings[q.Path[0]], "::") == "sqlx::"+q.Path[0] {
					return "use sqlx::", true
				}
			}
			if scopes[s].Parent == s {
				break
			}
			s = scopes[s].Parent
		}
	}
	return "", false
}

// role is executes when an execution method call of the same owner holds
// the site (chained, or the site as its argument), else builds. For a site
// in a local function (no node of its own, so owned by the enclosing
// function) the call must also be inside that local function: a call of
// the enclosing function around the local function's item does not run
// the site.
func (l sqlxFile) role(q facts.RustQuery) string {
	for _, c := range l.facts.Calls {
		if sqlxExecutions[c.StaticName] && c.Source == q.Owner && contains(c.Span, q.Span) && (q.Local == nil || contains(*q.Local, c.Span)) {
			return RoleExecutes
		}
	}
	return RoleBuilds
}

// claimSqlxMacro is row CT-S1's branch: a checked macro's literal through
// the reader against the package's migration set, fact (sqlx checks the
// text against the migrated database at compile time).
func (l sqlxFile) claimSqlxMacro(q facts.RustQuery, spelling string) site {
	name := q.Path[len(q.Path)-1]
	base := strings.TrimSuffix(name, "_unchecked")
	l.in.tag("CT-S1/" + base + "!")
	if base != name {
		l.in.tag("CT-S1/_unchecked")
	}
	l.in.tag("CT-S1/" + spelling)
	s := l.newSite(q, "CT-S1")
	s.fact = true
	l.readSite(&s, q)
	return s
}

// sqlxFunctionForms maps a query function to its CT-S2 form.
var sqlxFunctionForms = map[string]string{"query": "sqlx::query(", "query_as": "query_as::<_, T>(", "query_scalar": "query_scalar(", "query_with": "query_with", "query_as_with": "query_as_with"}

// claimSqlxFunction is row CT-S2's branch: as CT-S1, but inferred (TA-V1):
// the functions' text is not checked at compile time.
func (l sqlxFile) claimSqlxFunction(q facts.RustQuery) site {
	l.in.tag("CT-S2/" + sqlxFunctionForms[q.Path[len(q.Path)-1]])
	s := l.newSite(q, "CT-S2")
	s.assumes = []string{"TA-V1"}
	l.readSite(&s, q)
	return s
}

func (l sqlxFile) newSite(q facts.RustQuery, row string) site {
	return site{file: l.file, span: q.Span, owner: q.Owner, row: row, tool: "sqlx", rows: []string{row},
		role: l.role(q), verification: rowByID(row).Verification}
}

// unknown records a refusal on the site.
func (s *site) refuse(reason string, partial bool, candidates []string) {
	s.unknown = &Unknown{Row: s.row, Line: s.span.StartLine, Span: s.span, Tool: s.tool, Reason: reason, Partial: partial, Candidates: candidates}
}

// readSite runs the reader (CT-Q1) and name resolution (CT-Q2) for a
// site's literal against its package's migration sets.
func (l sqlxFile) readSite(s *site, q facts.RustQuery) {
	if !q.Literal {
		s.refuse(ReasonNotLiteral, false, nil)
		return
	}
	s.sql = q.SQL
	var usable []*migrationSet
	pkg := cargoPackage(l.file, l.in.Sources)
	for _, set := range l.byPackage[pkg] {
		if pkg != "" && set.dialect == "postgres" && len(set.tables) > 0 {
			usable = append(usable, set)
		}
	}
	if len(usable) == 0 {
		detail := "no sqlx migration set in the package"
		if pkg != "" && len(l.byPackage[pkg]) > 0 {
			detail = "no readable Postgres migration set in the package"
		}
		s.refuse(ReasonNoSchemaSource+": "+detail, false, nil)
		return
	}
	s.sets = usable
	readSQLSite(l.in, s, q.SQL, usable)
}

// readSQLSite runs the reader (CT-Q1) and name resolution (CT-Q2) over a
// carrier's literal against the schema sources usable for its site, and
// records the claims and refusals on the site.
func readSQLSite(in Input, s *site, sql string, usable []*migrationSet) {
	reading := readSQL(sql)
	in.tag("CT-Q1")
	if reading.Refusal != "" {
		s.refuse(ReasonOutsideSubset+": "+reading.Refusal, false, nil)
		return
	}
	for _, f := range reading.Forms {
		in.tag("CT-Q1/" + f)
	}
	s.rows = append(s.rows, "CT-Q1")
	if len(reading.Refs) == 0 {
		return
	}
	res := resolveNames(reading, usable)
	in.tag("CT-Q2")
	for _, f := range res.forms {
		in.tag("CT-Q2/" + f)
	}
	s.rows = append(s.rows, "CT-Q2")
	sort.Strings(s.rows)
	s.reads, s.writes, s.tables = res.reads, res.writes, res.tables
	if len(res.unresolved) == 0 {
		return
	}
	partial := len(res.reads)+len(res.writes) > 0
	code, _, _ := strings.Cut(res.unresolved[0].reason, ": ")
	var details []string
	var candidates []string
	allAmbiguous := true
	for _, u := range res.unresolved {
		c, d, _ := strings.Cut(u.reason, ": ")
		if c != code {
			// Mixed codes: the first names the record, every detail is kept.
			d = c + ": " + d
		}
		details = append(details, d)
		allAmbiguous = allAmbiguous && c == ReasonAmbiguousGroup
		candidates = append(candidates, u.candidates...)
	}
	if !allAmbiguous {
		candidates = nil
	} else {
		sort.Strings(candidates)
	}
	s.refuse(code+": "+strings.Join(details, ", "), partial, candidates)
}
