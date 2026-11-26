package prisma

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/lockfile"
)

// schema is one discovered Prisma schema (all files of a multi-file
// schema) with the schema-level facts the rows read: the datasource's
// provider, relation mode and schemas list (PR-B1), the generator's
// preview features (PR-B2), its enums (PR-E1) and the model tables built
// from it (PR-B3 and the field and block attribute rows).
type schema struct {
	pkg   *Package
	steps *models.Steps
	// major is the proven Prisma major the package's rows apply for
	// (developer decision 2026-11-09: Prisma 5 and 6 are read too; rows
	// whose behaviour differs by version gate on it from step G1B3 on).
	major int
	// version is the exact locked Prisma CLI version ("" when not proven);
	// the minor-version gates read it through since.
	version string
	// provider is the datasource's literal provider when it is one of the
	// SQL providers the contract lists, else "" (provider-dependent facts
	// unknown, row PR-B1).
	provider       string
	providerReason string
	// relationMode is "foreignKeys" or "prisma"; "" when not literal.
	relationMode string
	// schemas is the datasource's literal schemas list; schemasListed says
	// whether the key is present, schemasKnown whether it is literal.
	schemas                     []string
	schemasListed, schemasKnown bool
	previewFeatures             map[string]bool
	models                      map[string]*Block
	enums                       map[string]*enumInfo
	others                      map[string]string
	// byModel holds every model's table while it is built and linked
	// (rows PR-R1, PR-R3); a model refused by PR-P1 or PR-AA4 has none.
	byModel map[string]*table
	tables  []*models.Table
}

// since says whether the schema's Prisma version is proven to be at least
// major.minor: a later major, or the same major with an exact locked
// version of at least that minor.
func (s *schema) since(major, minor int) bool {
	if s.major != major {
		return s.major > major
	}
	return s.version != "" && lockfile.AtLeast(s.version, major, minor)
}

// enumInfo is an enum of the schema (row PR-E1): its database name and
// values after @map/@@map, nil when not known.
type enumInfo struct {
	name     string
	dbName   *string
	values   []string
	valuesOK bool
	// byName maps a value's name to its database name.
	byName map[string]string
	schema *string
	reason string
	// failed is set when the enum breaks a PR-P1 rule (its @@schema), so
	// Prisma rejects the schema.
	failed bool
}

// multiSchema says whether the @@schema rules of row PR-AA5 are in force
// (known) for the schema's version: multiSchema is GA from Prisma 6.13.0
// (prisma-engines 4c9c25c84c) and before it needs the multiSchema preview
// feature (5.22.0 psl validations/datasource.rs:5-16 and models.rs:277-283
// check the feature first). Without the feature and with a Prisma 6 minor
// that is not proven, whether the rules apply is not known.
func (s *schema) multiSchema() (inForce, known bool) {
	switch {
	case s.previewFeatures["multiSchema"] || s.since(6, 13):
		return true, true
	case s.major == 6 && s.version == "":
		return false, false
	}
	return false, true
}

// readSchema parses a package's schema files and builds its tables. A
// schema with a parse error, more than one datasource or provider mongodb
// yields no table (Prisma loads no schema, or creates no table).
func readSchema(p *Package, sources map[string][]byte, steps *models.Steps) *schema {
	s := &schema{pkg: p, steps: steps, major: p.Major, version: p.Version, byModel: map[string]*table{}, models: map[string]*Block{}, enums: map[string]*enumInfo{}, others: map[string]string{}, previewFeatures: map[string]bool{}}
	var files []*File
	failed := false
	for _, path := range p.Files {
		f := Parse(path, sources[path])
		for i, e := range f.Errors {
			if i == 3 {
				diagf(steps, path, "warning", codeImportFailure, "%s: %d more parse errors", path, len(f.Errors)-3)
				break
			}
			diagf(steps, path, "warning", codeImportFailure, "%s:%d:%d: Prisma schema parse error (%s); Prisma loads no schema, so nothing of %s is claimed", path, e.Line, e.Col, e.Message, p.SchemaPath)
		}
		failed = failed || len(f.Errors) > 0
		files = append(files, f)
	}
	if failed {
		return nil
	}
	type located struct {
		file string
		b    *Block
	}
	var datasources []located
	var modelList []located
	var views []located
	names := map[string]located{}
	for _, f := range files {
		for _, b := range f.Blocks {
			if b.Keyword == KeywordModel || b.Keyword == KeywordEnum || b.Keyword == KeywordView || b.Keyword == KeywordType {
				if first, dup := names[b.Name]; dup {
					tag("PR-P1")
					diagf(s.steps, f.Path, "warning", codeImportFailure, "%s:%d: PR-P1: %s %s is also defined at %s:%d; Prisma rejects the schema, so nothing of %s is claimed", f.Path, b.StartLine, b.Keyword, b.Name, first.file, first.b.StartLine, p.SchemaPath)
					return nil
				}
				names[b.Name] = located{f.Path, b}
			}
			if b.Keyword == KeywordView {
				views = append(views, located{f.Path, b})
			}
			switch b.Keyword {
			case KeywordDatasource:
				datasources = append(datasources, located{f.Path, b})
			case KeywordGenerator:
				s.readGenerator(b)
			case KeywordModel:
				s.models[b.Name] = b
				modelList = append(modelList, located{f.Path, b})
			case KeywordEnum:
				s.enums[b.Name] = nil
			case KeywordView, KeywordType:
				s.others[b.Name] = b.Keyword
				tag("PR-B4/" + b.Keyword)
				what := "a view creates nothing in Migrate"
				if b.Keyword == KeywordType {
					what = "composite types are MongoDB-only"
				}
				diagf(s.steps, f.Path, "info", codeUnread, "%s:%d: %s %s is not read (%s)", f.Path, b.StartLine, b.Keyword, b.Name, what)
			}
		}
	}
	switch len(datasources) {
	case 0:
		s.providerReason = "the schema has no datasource block, so the provider is not known"
		s.relationMode = ""
	case 1:
		if !s.readDatasource(datasources[0].file, datasources[0].b) {
			return nil
		}
		if inForce, known := s.multiSchema(); s.schemasListed && known && !inForce {
			tag("PR-P1")
			diagf(s.steps, datasources[0].file, "warning", codeImportFailure, "%s:%d: PR-P1: the datasource lists schemas without the multiSchema preview feature (Prisma %d before 6.13.0); Prisma rejects the schema, so nothing of %s is claimed", datasources[0].file, datasources[0].b.StartLine, s.major, p.SchemaPath)
			return nil
		}
	default:
		tag("PR-P1")
		diagf(s.steps, datasources[1].file, "warning", codeImportFailure, "%s:%d: PR-P1: more than one datasource block; Prisma rejects the schema, so nothing of %s is claimed", datasources[1].file, datasources[1].b.StartLine, p.SchemaPath)
		return nil
	}
	if len(views) > 0 && !s.previewFeatures["views"] {
		v := views[0]
		tag("PR-P1")
		diagf(s.steps, v.file, "warning", codeImportFailure, "%s:%d: PR-P1: view %s without the views preview feature; Prisma rejects the schema, so nothing of %s is claimed", v.file, v.b.StartLine, v.b.Name, p.SchemaPath)
		return nil
	}
	for _, f := range files {
		for _, b := range f.Blocks {
			if b.Keyword == KeywordEnum {
				s.enums[b.Name] = s.readEnum(f.Path, b)
				if s.enums[b.Name].failed {
					return nil
				}
			}
		}
	}
	var built []*table
	for _, m := range modelList {
		m := m
		steps.Guard(m.file, "model", fmt.Sprintf("%s:%d: model %s", m.file, m.b.StartLine, m.b.Name), func() {
			if t := s.buildTable(m.file, m.b); t != nil {
				s.byModel[m.b.Name] = t
				built = append(built, t)
			}
		})
	}
	// Relations need every model's table: implicit many-to-many first (it
	// may refuse a model, PR-P1), then the owning relations (PR-R1).
	var joins []*models.Table
	steps.Guard(p.SchemaPath, "relations", "Prisma schema "+p.SchemaPath, func() { joins = s.manyToMany() })
	for _, t := range built {
		if s.byModel[t.b.Name] != t {
			continue
		}
		t := t
		steps.Guard(t.file, "relations", fmt.Sprintf("%s:%d: model %s", t.file, t.b.StartLine, t.b.Name), func() {
			t.foreignKeys()
			s.tables = append(s.tables, t.rec)
		})
	}
	s.tables = append(s.tables, joins...)
	return s
}

// readDatasource reads row PR-B1; it returns false for mongodb (no
// tables) and for a datasource Prisma rejects (PR-P1).
func (s *schema) readDatasource(file string, b *Block) bool {
	s.relationMode = "foreignKeys"
	var provider, mode, schemas *Property
	modes := 0
	for _, prop := range b.Properties {
		switch prop.Key {
		case "provider":
			provider = prop
		case "relationMode", "referentialIntegrity":
			// referentialIntegrity is the deprecated name Prisma 5, 6 and
			// 7 still accept (psl datasource_loader.rs get_relation_mode).
			mode = prop
			modes++
		case "schemas":
			schemas = prop
		case "url", "directUrl", "shadowDatabaseUrl":
			if s.major == 7 {
				// Prisma 7 moved the connection URLs to prisma.config.ts and
				// rejects them in the schema (P1012; psl
				// validate/datasource_loader.rs:132-142 at prisma-engines
				// 0edf323efd1d, checked with the 7.10.0 CLI): PR-P1 for the
				// whole schema.
				tag("PR-P1")
				diagf(s.steps, file, "warning", codeImportFailure, "%s:%d: PR-P1: schema rejected by the pinned Prisma version: the datasource property %s is not supported by Prisma 7 (connection URLs belong in prisma.config.ts); nothing of %s is claimed", file, prop.Line, prop.Key, s.pkg.SchemaPath)
				return false
			}
		}
	}
	switch v, ok := strLit(propValue(provider)); {
	case !ok:
		s.providerReason = "the datasource's provider is not a string literal"
	case v == providerMongo:
		tag("PR-B1")
		diagf(s.steps, file, "info", codeUnread, "%s:%d: provider mongodb: collections are out of scope, no table is claimed", file, provider.Line)
		return false
	case contains(sqlProviders, v):
		s.provider = v
		tag("PR-B1/" + v)
	default:
		s.providerReason = fmt.Sprintf("provider %q is not one the contract lists", v)
	}
	if modes > 1 {
		tag("PR-P1")
		diagf(s.steps, file, "warning", codeImportFailure, "%s:%d: PR-P1: relationMode and referentialIntegrity are both set; Prisma rejects the schema, so nothing is claimed", file, mode.Line)
		return false
	}
	if mode != nil {
		v, ok := strLit(mode.Value)
		if ok && (v == "foreignKeys" || v == "prisma") {
			s.relationMode = v
		} else {
			s.relationMode = ""
		}
	}
	if schemas != nil {
		s.schemasListed = true
		if v := propValue(schemas); v != nil && v.Kind == ExprArray {
			s.schemasKnown = true
			for _, item := range v.Items {
				name, ok := strLit(item)
				if !ok {
					s.schemasKnown = false
					break
				}
				s.schemas = append(s.schemas, name)
			}
		}
	}
	return true
}

func propValue(p *Property) *Expr {
	if p == nil {
		return nil
	}
	return p.Value
}

// readGenerator reads a literal previewFeatures list (row PR-B2); any
// other value counts as no preview feature.
func (s *schema) readGenerator(b *Block) {
	for _, prop := range b.Properties {
		if prop.Key != "previewFeatures" || prop.Value == nil || prop.Value.Kind != ExprArray {
			continue
		}
		features := map[string]bool{}
		for _, item := range prop.Value.Items {
			v, ok := strLit(item)
			if !ok {
				features = nil
				break
			}
			features[v] = true
		}
		if features != nil {
			tag("PR-B2")
		}
		for k := range features {
			s.previewFeatures[k] = true
		}
	}
}

// readEnum reads an enum (row PR-E1): @@map gives its database name, a
// value's @map its database value; anything else unread makes the part it
// could change unknown.
func (s *schema) readEnum(file string, b *Block) *enumInfo {
	e := &enumInfo{name: b.Name, valuesOK: true, byName: map[string]string{}}
	name := b.Name
	e.dbName = &name
	seen := map[string]bool{}
	for _, a := range b.Attributes {
		if seen[a.Name] {
			e.dbName, e.reason = nil, "@@"+a.Name+" is written twice"
			continue
		}
		seen[a.Name] = true
		switch a.Name {
		case "map":
			if v, ok := singleString(a); ok {
				e.dbName = &v
			} else {
				e.dbName, e.reason = nil, "the enum's @@map is not one string literal"
			}
		case "schema":
			if v, ok := singleString(a); ok {
				e.schema = &v
			} else {
				e.dbName, e.reason = nil, "the enum's @@schema is not one string literal"
			}
		default:
			e.dbName, e.reason = nil, "enum attribute @@"+a.Name+" is not read"
		}
	}
	for _, v := range b.Values {
		db := v.Name
		for _, a := range v.Attributes {
			if a.Name != "map" {
				e.valuesOK, e.reason = false, "enum value attribute @"+a.Name+" is not read"
				continue
			}
			if m, ok := singleString(a); ok {
				db = m
			} else {
				e.valuesOK, e.reason = false, "an enum value's @map is not one string literal"
			}
		}
		e.values = append(e.values, db)
		e.byName[v.Name] = db
	}
	if !e.valuesOK {
		e.values = nil
	}
	// PR-AA5 and PR-P1 for an enum: the same @@schema rules as a model.
	inForce, known := s.multiSchema()
	switch {
	case !s.schemasKnown && s.schemasListed:
		e.schema, e.dbName, e.reason = nil, nil, "the datasource's schemas list is not literal"
	case inForce && s.schemasListed && e.schema == nil && seen["schema"] == false:
		e.failed = true
		s.enumFailure(file, b, "the datasource lists schemas but the enum has no @@schema")
	case inForce && e.schema != nil && !contains(s.schemas, *e.schema):
		e.failed = true
		s.enumFailure(file, b, fmt.Sprintf("schema %q is not in the datasource's schemas list", *e.schema))
	case !known && e.schema != nil, !inForce && known && e.schema != nil:
		e.schema, e.dbName, e.reason = nil, nil, "@@schema is not read without the multiSchema preview feature at this Prisma version"
	}
	return e
}

func (s *schema) enumFailure(file string, b *Block, why string) {
	tag("PR-P1")
	diagf(s.steps, file, "warning", codeImportFailure, "%s:%d: PR-P1: enum %s: %s; Prisma rejects the schema, so nothing of %s is claimed", file, b.StartLine, b.Name, why, s.pkg.SchemaPath)
}

// baseAssumptions are the assumptions every table of the schema states;
// buildTable adds PR-V5 for a table in the default schema.
func (s *schema) baseAssumptions() []string {
	out := []string{"PR-V1", "PR-V2", "PR-V3", "PR-V4"}
	if s.mappedFacts() {
		out = append(out, "PR-V6")
	}
	return out
}

// providerVerification is the verification of a provider-dependent fact:
// oracle on postgresql and sqlite (the providers the oracle runs on),
// mapped on the mapped providers.
func (s *schema) providerVerification() string {
	if oracleProvider(s.provider) {
		return models.VerificationOracle
	}
	return models.VerificationMapped
}

// mappedFacts says whether the schema's tables hold facts from a mapped
// provider's connector source (verification "mapped"), so they rest on
// PR-V6.
func (s *schema) mappedFacts() bool {
	known, _ := s.providerDependentKnown()
	return known && mappedProvider(s.provider)
}

// Unique kinds: how a provider creates the unique index Prisma derives
// from @unique or @@unique without a predicate (and the implicit
// many-to-many join table's (A, B) key where it is not a primary key).
const (
	uniqueAsIndex      = "index"
	uniqueAsConstraint = "constraint"
)

// uniqueKind is that kind for the schema's provider, with the
// verification of the claim, or "" with the reason when not known. The
// kind is provider-dependent:
// postgresql and sqlite create a unique index (CREATE UNIQUE INDEX; the
// oracle), mysql a UNIQUE INDEX (flavour/mysql/renderer.rs:225-253 and
// sql-ddl mysql.rs:189-197, :333-340) and cockroachdb a unique index
// (the postgres renderer, flavour/postgres/renderer.rs:361-391, sql-ddl
// postgres.rs:365-368), both mapped; sqlserver a UNIQUE constraint
// (flavour/mssql/renderer.rs:154-166 and :211-242), mapped.
func (s *schema) uniqueKind() (kind, verification, why string) {
	if known, why := s.providerDependentKnown(); !known {
		return "", "", why
	}
	switch s.provider {
	case providerSQLServer:
		return uniqueAsConstraint, models.VerificationMapped, ""
	case providerPostgres, providerSQLite:
		return uniqueAsIndex, models.VerificationOracle, ""
	}
	return uniqueAsIndex, models.VerificationMapped, ""
}

// providerDependentKnown says whether provider-dependent facts can be
// claimed; why is the reason when not.
func (s *schema) providerDependentKnown() (bool, string) {
	switch {
	case s.provider == "":
		return false, s.providerReason
	case s.major != 7 && mappedProvider(s.provider):
		// The mapped providers' types and defaults are cited from Prisma
		// 7's connector source; their renderers changed since 5.22.0
		// (developer decision 2026-11-09: what is not checked for the
		// locked version stays unknown).
		return false, fmt.Sprintf("the %s mapping is cited from Prisma 7's connector source; Prisma %d's is not checked", s.provider, s.major)
	}
	return true, ""
}

// strLit returns a string literal's value.
func strLit(e *Expr) (string, bool) {
	if e == nil || e.Kind != ExprString {
		return "", false
	}
	return e.Value, true
}

// args splits an attribute's arguments: positional values in order and
// named values by name. ok is false when a name is repeated or a named
// argument is empty (`name:`), which Prisma rejects.
type args struct {
	pos   []*Expr
	named map[string]*Expr
	order []string
	ok    bool
}

func splitArgs(list *ArgList) args {
	out := args{named: map[string]*Expr{}, ok: true}
	if list == nil {
		return out
	}
	for _, a := range list.Args {
		if a.Value == nil {
			out.ok = false
			continue
		}
		if a.Name == "" {
			out.pos = append(out.pos, a.Value)
			continue
		}
		if _, dup := out.named[a.Name]; dup {
			out.ok = false
		}
		out.named[a.Name] = a.Value
		out.order = append(out.order, a.Name)
	}
	return out
}

// singleString reads an attribute whose only argument is one positional
// string literal (@map("x"), @@map("t"), @@schema("s")).
func singleString(a *Attribute) (string, bool) {
	x := splitArgs(a.Args)
	if !x.ok || len(x.pos) != 1 || len(x.named) != 0 {
		return "", false
	}
	return strLit(x.pos[0])
}

// noArgs says whether an attribute has no arguments (`@x` or `@x()`).
func noArgs(a *Attribute) bool { return a.Args == nil || len(a.Args.Args) == 0 }

// nativeName returns the native type name of a `@db.<Name>` attribute.
func nativeName(a *Attribute) (string, bool) {
	if !strings.HasPrefix(a.Name, "db.") || strings.Count(a.Name, ".") != 1 {
		return "", false
	}
	return strings.TrimPrefix(a.Name, "db."), true
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
