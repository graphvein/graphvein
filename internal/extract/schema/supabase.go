package schema

// Supabase migration sets (T024c1, ADR 0077; claim contract rows SB-M1 to
// SB-M3 in docs/contract/claims/nextjs-supabase.md). A Supabase project keeps
// its migrations in supabase/migrations beside supabase/config.toml. The
// Supabase CLI (pkg/migration) lists the directory's direct entries in
// lexical order, applies the files named <digits>_<name>.sql and skips a
// legacy first <14 digits>_init.sql older than 20211209000000. Each file
// runs as one pgx batch with its bookkeeping row in
// supabase_migrations.schema_migrations, which PostgreSQL runs in one
// implicit transaction. The statements are read by the same PostgreSQL 17
// reader as sqlx sets (ADR 0047, 0051); only discovery, ordering, the
// block a file runs in, the bookkeeping table and three Supabase rows
// differ:
//
//   - SB-M1 discovery and order (supabaseDiscover, orderSupabase);
//   - SB-M2 the platform objects the Supabase prelude provides (assumption
//     SB-V2): a foreign key to one of their keys resolves to an external
//     platform object instead of an unresolved reference;
//   - SB-M3 row level security: ENABLE, DISABLE, FORCE and NO FORCE ROW
//     LEVEL SECURITY and CREATE, ALTER and DROP POLICY are schema-neutral
//     (no partial table) and recorded as the table attributes rls and
//     policies, as written; no claim about roles, USING or WITH CHECK;
//   - SB-M4 (T024c1b) CREATE EXTENSION of an extension the Supabase
//     platform lets its migration role create (assumption SB-V4) is not a
//     superuser-only statement there (E6, Y51 do not apply); it stays an
//     opaque statement (O2);
//   - SB-M5 (T024c1b) a supabase/migrations directory without config.toml
//     whose direct entries all follow the CLI's name pattern is read as a
//     Supabase set too (assumption SB-V3).
//   - SB-M6 to SB-M8 (T024c1b, T024c1c): platform extensions' known
//     effects, inserts into storage.buckets, storage functions;
//   - SB-M9 (NX-F2) data statements whose code cannot change row level
//     security keep the rls flags known;
//   - SB-M10 (NX-F2) column types created by an SB-M6 extension.
//
// Every other form is read as in sqlx sets (the reader's own rows and its
// default branch D).

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

const (
	toolSqlx     = "sqlx"
	toolSupabase = "supabase"
)

// supabaseRow is one row of the Supabase migration contract (SB-M1 to
// SB-M10). The authoritative wording is the task's table;
// TestSupabaseContract keeps the two and the code's branches 1:1.
type supabaseRow struct {
	id, form, claim string
	// branch is the function that implements the row.
	branch string
}

// supabaseRows lists the rows in the task's order.
var supabaseRows = []supabaseRow{
	{"SB-M1", "a directory supabase/ holding config.toml and migrations/; members per the Supabase CLI: direct entries in lexical order, name ^([0-9]+)_(.*)\\.sql$, the first entry skipped when it is a <14 digits>_init.sql older than 20211209000000",
		"a migration set, tool supabase, dialect postgres, read by the PostgreSQL 17 statement reader", "supabaseDiscover"},
	{"SB-M2", "a foreign key to a key of a prelude table (auth.users, storage.objects, storage.buckets) of a matching type",
		"resolves to an external platform object (assumes SB-V2): no schema_unresolved_reference, no partial table", "platformReference"},
	{"SB-M3", "ALTER TABLE t ENABLE / DISABLE / FORCE / NO FORCE ROW LEVEL SECURITY; CREATE POLICY p ON t ...; DROP POLICY [IF EXISTS] p ON t; ALTER POLICY p ON t RENAME TO q (other ALTER POLICY forms keep the policy)",
		"schema-neutral; table attributes rls {enabled, forced} and policies [{name, command, table}] as written", "rowSecurity"},
	{"SB-M4", "CREATE EXTENSION [IF NOT EXISTS] x ..., top level or run by code, in a Supabase set, x one of the platform extensions (http, vector)",
		"not a statement only a superuser may run (assumes SB-V4); the statement stays opaque (O2) and its other preconditions apply", "platformExtension"},
	{"SB-M5", "a directory supabase/migrations without config.toml beside it, holding at least one entry, every direct entry named per ^([0-9]+)_(.*)\\.sql$, not a sqlx set",
		"a Supabase set as SB-M1 (assumes SB-V3), package the migrations directory", "supabaseBare"},
	{"SB-M6", "CREATE EXTENSION [IF NOT EXISTS] x [WITH] SCHEMA s at top level in a Supabase set, x one of the platform extensions, without VERSION, FROM or CASCADE, s not public or pg_catalog, the set creating none of x's object names and no operator, cast, operator class or family, access method or aggregate",
		"a known effect (supabaseExtensionObjects: types, domains, functions, aggregates, operators, operator classes, casts and access methods in s, no relation): no opaque statement, so later statements are not made uncertain (assumes SB-V4)", "platformExtensionEffect"},
	{"SB-M7", "INSERT INTO storage.buckets ... in a Supabase set",
		"its column references are not checked: the insert succeeds on a fresh project and the platform's triggers change no object the set holds (assumes SB-V5, whose text names the statement); when no statement of the set or its code names buckets otherwise or runs a dynamic EXECUTE, no trigger of the set fires on it, so it is no opaque data change", "platformInsert"},
	{"SB-M8", "a call storage.foldername(<text>) or storage.filename(<text>) in a LANGUAGE sql body R1b.P5's analysed form reads, in a Supabase set that creates no function of that name",
		"the platform function's signature as the Supabase storage migrations define it (assumes SB-V2): foldername(name text) returns text[], filename(name text) returns text", "platformFunction"},
	{"SB-M9", "INSERT, UPDATE or DELETE in a Supabase set, opaque only as a data change in a set that defines code (O1), when every earlier opaque statement of the set is such a statement, every statement was read, its P12 inventory is bounded and holds no statement, and its target and every data target of its inventory is a table the set holds under a certain name (qualified by its schema, or by public for one created unqualified, and no other relation of the name), and no CREATE [OR REPLACE] RULE came before it at top level or is in the set's code (a rule's actions are not read; stacked gate NX-S), and no earlier statement ran code the pass cannot read or created an event trigger, and none in the set's code creates one (2026-11-18: either ends SB-M9 for the set, every table's rls flags and policies unknown to its end)",
		"it cannot change any table's row level security: the rls flags and policies stay known across it; the statement stays opaque for every other fact. A data statement (top level, or in the bounded inventory of an O1 statement) on any other relation, a pg_catalog relation among them, and an unbounded inventory make every held table's rls flags and policies unknown (rlsForeignData); so do a top-level CREATE [OR REPLACE] RULE and every data statement after it, or every data statement of a set whose code creates a rule (rlsRuleStatement)", "rlsInertData"},
	{"SB-M10", "a column type (CREATE TABLE, ADD COLUMN) naming a type of an SB-M6 extension (vector, halfvec, sparsevec; http_method, content_type, http_header, http_response, http_request; their arrays), qualified by the extension's schema, or unqualified under a known search_path that reaches that schema (E4's value: \"$user\", public, extensions), when the set and its code create no type or relation of the name, no opaque statement ran since the extension and no ALTER or DROP names the type",
		"the type exists (M2.P1 proven, assumes SB-V2 and SB-V4); its modifiers per the type's typmod_in: vector and halfvec one integer 1 to 16000, sparsevec 1 to 1000000000, otherwise refused; the http types none, otherwise refused; other modifiers not proven", "platformColumnType"},
}

// supabaseAssumption is the environment assumption behind SB-M2.
const supabaseAssumption = "SB-V2"

// supabaseExtensions are the extensions SB-M4 takes as available to the
// migration role on the Supabase platform (assumption SB-V4). Supabase's
// own allow-list (supautils' privileged extensions in the supabase/postgres
// image configuration) is not published where the pass's sources can be
// read, so the list holds only the extensions the corpora create, each
// checked by replaying the corpus on PostgreSQL 17 with the PGDG builds of
// the extension (verification mapped; T024c1b).
var supabaseExtensions = map[string]bool{"http": true, "vector": true}

// supabaseExtensionObjects are the names each platform extension's
// install script creates (SB-M6), read from the PGDG builds the corpus was
// replayed with: pgvector 0.8.7 (vector--0.8.7.sql) and pgsql-http 1.7
// (http--1.7.sql). Neither script creates a relation, schema, trigger or
// event trigger, nor runs a statement on existing objects; neither
// declares an implicit cast from a pg_catalog type or a polymorphic
// argument, so no later expression over pg_catalog types resolves to one
// of their functions or operators. The lists hold the types, domains,
// functions, aggregates and access methods by name (with each type's
// array type), the names a set statement could collide with; operators,
// casts and operator classes collide only with statements SB-M6 excludes.
var supabaseExtensionObjects = map[string][]string{
	"http": {"bytea_to_text", "content_type", "http", "http_delete", "http_get", "http_head", "http_header", "http_headers",
		"http_list_curlopt", "http_method", "http_patch", "http_post", "http_put", "http_request", "http_reset_curlopt",
		"http_response", "http_set_curlopt", "text_to_bytea", "urlencode",
		"_content_type", "_http_header", "_http_method", "_http_request", "_http_response"},
	"vector": {"array_to_halfvec", "array_to_sparsevec", "array_to_vector", "avg", "binary_quantize", "cosine_distance",
		"halfvec", "halfvec_accum", "halfvec_add", "halfvec_avg", "halfvec_cmp", "halfvec_combine", "halfvec_concat",
		"halfvec_eq", "halfvec_ge", "halfvec_gt", "halfvec_in", "halfvec_l2_squared_distance", "halfvec_le", "halfvec_lt",
		"halfvec_mul", "halfvec_ne", "halfvec_negative_inner_product", "halfvec_out", "halfvec_recv", "halfvec_send",
		"halfvec_spherical_distance", "halfvec_sub", "halfvec_to_float4", "halfvec_to_sparsevec", "halfvec_to_vector",
		"halfvec_typmod_in", "hamming_distance", "hnsw", "hnsw_bit_support", "hnsw_halfvec_support", "hnsw_sparsevec_support",
		"hnswhandler", "inner_product", "ivfflat", "ivfflat_bit_support", "ivfflat_halfvec_support", "ivfflathandler",
		"jaccard_distance", "l1_distance", "l2_distance", "l2_norm", "l2_normalize", "sparsevec", "sparsevec_cmp",
		"sparsevec_eq", "sparsevec_ge", "sparsevec_gt", "sparsevec_in", "sparsevec_l2_squared_distance", "sparsevec_le",
		"sparsevec_lt", "sparsevec_ne", "sparsevec_negative_inner_product", "sparsevec_out", "sparsevec_recv",
		"sparsevec_send", "sparsevec_to_halfvec", "sparsevec_to_vector", "sparsevec_typmod_in", "subvector", "sum",
		"vector", "vector_accum", "vector_add", "vector_avg", "vector_cmp", "vector_combine", "vector_concat", "vector_dims",
		"vector_eq", "vector_ge", "vector_gt", "vector_in", "vector_l2_squared_distance", "vector_le", "vector_lt",
		"vector_mul", "vector_ne", "vector_negative_inner_product", "vector_norm", "vector_out", "vector_recv",
		"vector_send", "vector_spherical_distance", "vector_sub", "vector_to_float4", "vector_to_halfvec",
		"vector_to_sparsevec", "vector_typmod_in", "_halfvec", "_sparsevec", "_vector"},
}

// createsOperatorObject reports a statement that creates an operator,
// cast, operator class or family, access method or aggregate (SB-M6's
// exclusion).
func createsOperatorObject(toks []token) bool {
	c := &cursor{toks: toks}
	if !c.acceptKw("create") {
		return false
	}
	c.acceptKw("or", "replace")
	return c.peekKw("operator") || c.peekKw("cast") || c.peekKw("access", "method") || c.peekKw("aggregate")
}

// platformExtensionEffect applies SB-M6: in a Supabase set, CREATE
// EXTENSION of a platform extension into a named schema other than public
// and pg_catalog, without VERSION, FROM or CASCADE, is a known effect
// when the set creates none of the extension's object names and no
// operator-like object: the statement is no longer opaque. Under SB-V4 it
// also succeeds (SB-M4). Any other form stays opaque (O2).
func (s *state) platformExtensionEffect(toks []token, a *analysis) {
	if a.class != classOpaque || a.opaqueKind != "extension" || s.operatorObjects {
		return
	}
	c := &cursor{toks: toks}
	if !c.acceptKw("create", "extension") {
		return
	}
	c.acceptKw("if", "not", "exists")
	n, ok := extensionName(c)
	objects, listed := supabaseExtensionObjects[n]
	if !ok || !listed {
		return
	}
	c.acceptKw("with")
	if !c.acceptKw("schema") {
		return
	}
	schema, err := c.ident()
	if err != nil || !c.done() || schema == "public" || schema == "pg_catalog" {
		return
	}
	for _, o := range objects {
		if s.names.created[o] != 0 {
			return
		}
	}
	a.class, a.reason = classNoEffect, ""
	s.set.supabaseRows["SB-M6"]++
	// SB-M10: its types are column types in schema.
	s.recordPlatformTypes(n, schema)
}

// platformInsert applies SB-M7 to INSERT INTO storage.buckets in a
// Supabase set: its column references are not checked (the platform
// table's columns are not the pass's), and the insert succeeds on a fresh
// project (assumption SB-V5, which names the statement). It reports
// whether the row applies.
func (s *state) platformInsert(toks []token) bool {
	if s.set.tool != toolSupabase {
		return false
	}
	if !isBucketInsert(toks) {
		return false
	}
	text := statementTextUpTo(toks, s.m.source, 400)
	if !slices.Contains(s.set.platformInserts, text) {
		s.set.platformInserts = append(s.set.platformInserts, text)
	}
	s.set.supabaseRows["SB-M7"]++
	return true
}

// mayTouchBuckets reports a statement (top level or of code) that names
// an object buckets other than as INSERT INTO storage.buckets or as
// REFERENCES storage.buckets, or runs an EXECUTE that is no trigger's
// EXECUTE FUNCTION / PROCEDURE, in its own text or in the body of a DO
// block or routine it creates (a body in another language, or one the
// pass cannot read, counts as such): it may attach a trigger or rule to
// storage.buckets (SB-M7's exclusion).
func mayTouchBuckets(toks []token, src []byte) bool {
	if isBucketInsert(toks) {
		return false
	}
	if bucketWords(toks) {
		return true
	}
	var b *routineBody
	if len(toks) > 0 && toks[0].kind == tokIdent && toks[0].value == "do" {
		b = doBody(toks, src)
	} else {
		_, b = routineBodyOf(toks, src)
	}
	switch {
	case b == nil:
		return false
	case b.unread != "" || b.lang != "plpgsql" && b.lang != "sql":
		return true
	case b.toks != nil:
		return bucketWords(b.toks)
	}
	body, ok := lexBody(b.text)
	return !ok || bucketWords(body)
}

// bucketWords reports the words mayTouchBuckets looks for in a token list.
func bucketWords(toks []token) bool {
	for i, t := range toks {
		if t.kind != tokIdent && t.kind != tokQuoted {
			continue
		}
		if t.value == "buckets" {
			// A foreign key to storage.buckets adds RI triggers there
			// that fire on UPDATE and DELETE only (ri_triggers.c), never
			// on INSERT.
			if i >= 3 && toks[i-3].kind == tokIdent && toks[i-3].value == "references" && toks[i-2].value == "storage" && toks[i-1].value == "." {
				continue
			}
			if isBucketInsert(toks[max(0, i-3):]) {
				continue
			}
			return true
		}
		if t.kind == tokIdent && t.value == "execute" && !(i+1 < len(toks) && toks[i+1].kind == tokIdent && (toks[i+1].value == "function" || toks[i+1].value == "procedure")) {
			return true
		}
	}
	return false
}

// isBucketInsert reports INSERT INTO storage.buckets.
func isBucketInsert(toks []token) bool {
	c := &cursor{toks: toks}
	if !c.acceptKw("insert", "into") {
		return false
	}
	q, err := c.name()
	return err == nil && q.schema != nil && *q.schema == "storage" && q.name == "buckets"
}

// platformInsertEffect applies SB-M7's effect: an INSERT INTO
// storage.buckets that would be an opaque data change only because the
// set defines triggers is no opaque statement when no statement of the set
// or its code may make a trigger or rule fire on storage.buckets; under
// SB-V5 the platform's own triggers change no object the set holds.
func (s *state) platformInsertEffect(toks []token, a *analysis) {
	if a.class != classOpaque || a.opaqueKind != "dml" || s.bucketCode || !isBucketInsert(toks) {
		return
	}
	a.class, a.reason = classNoEffect, ""
}

// supabaseInsertText is the text of assumption SB-V5 for one statement.
func supabaseInsertText(stmt string) string {
	return "inserts into Supabase platform tables (storage.buckets) succeed on a fresh project, and the platform's triggers change no object of the set: " + stmt
}

// supabaseBareText and supabaseExtensionText are the texts of the
// assumptions SB-V3 and SB-V4 the schema view shows for a set.
const supabaseBareText = "read as Supabase CLI migrations; no supabase/config.toml found"

func supabaseExtensionText(x string) string {
	return "Supabase platform: extension " + x + " is available to the migration role"
}

// supabaseAssumptions are the set's assumptions beyond SB-V2, as the
// platform attribute lists them: SB-V3 for a bare directory, SB-V4 for
// each platform extension the set creates.
func (set *migrationSet) supabaseAssumptions() []any {
	out := []any{}
	if set.bare {
		out = append(out, map[string]any{"id": "SB-V3", "text": supabaseBareText})
	}
	for _, x := range set.platformExtensions {
		out = append(out, map[string]any{"id": "SB-V4", "text": supabaseExtensionText(x)})
	}
	for _, x := range set.platformInserts {
		out = append(out, map[string]any{"id": "SB-V5", "text": supabaseInsertText(x)})
	}
	return out
}

// platformExtension applies SB-M4 to a statement Y51 reads as CREATE
// EXTENSION of an extension that is not trusted: in a Supabase set, an
// extension of the platform list is available to the migration role, so
// the statement is not superuser-only. It reports whether the row applies.
func (s *state) platformExtension(toks []token, src []byte) bool {
	if s.set.tool != toolSupabase {
		return false
	}
	c := &cursor{toks: toks, src: src}
	if !c.acceptKw("create", "extension") {
		return false
	}
	c.acceptKw("if", "not", "exists")
	n, ok := extensionName(c)
	if !ok || !supabaseExtensions[n] {
		return false
	}
	if !slices.Contains(s.set.platformExtensions, n) {
		s.set.platformExtensions = append(s.set.platformExtensions, n)
		sort.Strings(s.set.platformExtensions)
	}
	s.set.supabaseRows["SB-M4"]++
	return true
}

// supabasePlatform lists what the Supabase platform provides before the
// first user migration runs (SB-V2), as the oracle's prelude
// (scripts/oracle/nextjs_supabase/supabase-prelude.sql) stubs it. The
// schema view shows it as the set's environment assumption.
var supabasePlatform = []string{
	"schemas auth, storage, extensions, realtime, vault",
	"roles anon, authenticated, service_role",
	"tables auth.users (id uuid primary key), storage.buckets (id text primary key), storage.objects (id uuid primary key)",
	"functions auth.uid(), auth.role(), auth.jwt(), storage.foldername(text), storage.filename(text)",
}

// platformFunctions are the SB-M8 platform functions and their one
// signature (argument type, result type), read from the Supabase storage
// API's tenant migrations (github.com/supabase/storage, Apache-2.0, at
// 132a66ae: 0002-storage-schema.sql defines both, 0036, 0060 and 0061
// replace their bodies with the same signature; no migration adds an
// overload). Verification mapped: the oracle's prelude stubs them with
// the same signatures.
var platformFunctions = map[string][2]string{
	"storage.foldername": {"text", "text[]"},
	"storage.filename":   {"text", "text"},
}

// platformFunction applies SB-M8: in a Supabase set that creates no
// function of the name, schema.name is a platform function with the
// signature of platformFunctions (assumption SB-V2). The caller counts
// the row when the body it proves uses the call.
func (s *state) platformFunction(schema, name string) (arg, ret string, ok bool) {
	sig, listed := platformFunctions[schema+"."+name]
	if s.set.tool != toolSupabase || !listed || s.names.created[name]&kindFunction != 0 || s.ledger[name]&kindFunction != 0 {
		return "", "", false
	}
	return sig[0], sig[1], true
}

// platformKeys are the prelude tables and the type of their primary key,
// the only key SB-M2 resolves a foreign key to.
var platformKeys = map[string]string{
	"auth.users":      "uuid",
	"storage.objects": "uuid",
	"storage.buckets": "text",
}

var (
	// supabaseName is the CLI's migrateFilePattern.
	supabaseName = regexp.MustCompile(`^([0-9]+)_(.*)\.sql$`)
	// supabaseInit is the CLI's initSchemaPattern (unanchored, as there).
	supabaseInit = regexp.MustCompile(`([0-9]{14})_init\.sql`)
)

// supabaseLegacyInit is the CLI's shouldSkip: a first entry named
// <14 digits>_init.sql older than 20211209000000 is not applied.
func supabaseLegacyInit(name string) bool {
	m := supabaseInit.FindStringSubmatch(name)
	if len(m) != 2 {
		return false
	}
	v, err := strconv.ParseUint(m[1], 10, 64)
	return err == nil && v < 20211209000000
}

// supabaseDiscover finds the Supabase sets: every directory named supabase
// that holds config.toml and migration files (SB-M1), and every
// supabase/migrations directory without config.toml whose entries all
// follow the CLI's name pattern (SB-M5, supabaseBare). taken are the sqlx
// sets' directories, never a bare Supabase set.
func supabaseDiscover(in Input, taken map[string]bool, diagnostics *[]contract.Diagnostic) []*migrationSet {
	var configs []string
	bare := map[string]bool{}
	for p := range in.Sources {
		if path.Base(p) == "config.toml" && path.Base(path.Dir(p)) == "supabase" {
			configs = append(configs, p)
		}
		if dir := path.Dir(p); path.Base(dir) == "migrations" && path.Base(path.Dir(dir)) == "supabase" {
			bare[dir] = true
		}
	}
	sort.Strings(configs)
	var sets []*migrationSet
	for _, config := range configs {
		migDir := path.Join(path.Dir(config), "migrations")
		delete(bare, migDir)
		if set := supabaseSet(in, migDir, config, diagnostics); set != nil {
			sets = append(sets, set)
		}
	}
	var dirs []string
	for dir := range bare {
		if !taken[dir] {
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		if set := supabaseBare(in, dir, diagnostics); set != nil {
			sets = append(sets, set)
		}
	}
	return sets
}

// supabaseBare applies SB-M5: a supabase/migrations directory without
// config.toml is a Supabase set (assumption SB-V3) when it holds at least
// one entry and every direct entry is named as the CLI applies it; its
// package is the directory. Any other name there (a README, a down file,
// a subdirectory) or a directory that cannot be listed leaves it no set,
// as before.
func supabaseBare(in Input, migDir string, diagnostics *[]contract.Diagnostic) *migrationSet {
	entries, _, unlisted := supabaseEntries(in, migDir)
	if unlisted != "" || len(entries) == 0 {
		return nil
	}
	for _, name := range entries {
		if !supabaseName.MatchString(name) {
			return nil
		}
	}
	set := supabaseSet(in, migDir, migDir, diagnostics)
	if set == nil {
		return nil
	}
	set.bare = true
	set.supabaseRows = map[string]int{"SB-M5": 1}
	return set
}

// supabaseEntries lists a migrations directory's direct entries as the CLI
// lists them (sorted by name) and the regular files among them; without a
// listing (no root, or the listing failed), the scanned sources. unlisted
// is the error of a listing that failed for a reason other than absence.
func supabaseEntries(in Input, migDir string) (entries []string, regular map[string]bool, unlisted string) {
	regular = map[string]bool{}
	if in.Root != "" {
		dir := filepath.Join(in.Root, filepath.FromSlash(migDir))
		list, err := readDir(dir)
		switch {
		case err == nil:
			for _, e := range list {
				entries = append(entries, e.Name())
				if info, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && info.Mode().IsRegular() {
					regular[e.Name()] = true
				}
			}
			return entries, regular, ""
		case !errors.Is(err, fs.ErrNotExist):
			unlisted = err.Error()
		}
	}
	for file := range in.Sources {
		if rest, ok := strings.CutPrefix(file, migDir+"/"); ok && !strings.Contains(rest, "/") {
			entries = append(entries, rest)
			regular[rest] = true
		}
	}
	sort.Strings(entries)
	return entries, regular, unlisted
}

// supabaseSet reads one Supabase set's members by SB-M1's rule, or returns
// nil when the directory holds no member and nothing unreadable. pkg is
// the set's package: its config.toml, or the directory for a bare set.
func supabaseSet(in Input, migDir, pkg string, diagnostics *[]contract.Diagnostic) *migrationSet {
	set := &migrationSet{tool: toolSupabase, dir: migDir, cargo: pkg, dialect: "postgres", marker: markerIgnored, opaque: map[string]int{}, diagnostics: diagnostics, tests: in.Tests, sourceLookup: in.Sources}
	entries, regular, unlisted := supabaseEntries(in, migDir)
	set.unlisted = unlisted
	var members []string
	for i, name := range entries {
		if i == 0 && supabaseLegacyInit(name) {
			set.skipped = append(set.skipped, name)
			continue
		}
		if !supabaseName.MatchString(name) {
			continue
		}
		file := migDir + "/" + name
		if _, ok := in.Sources[file]; ok && regular[name] {
			members = append(members, name)
			continue
		}
		// The CLI reads every member when it applies it; one the scanner
		// did not keep (not UTF-8, ignored, a directory or a link) is as
		// sqlx's unreadable member (SB-M1 refusal), from which on applied
		// is unknown (orderSupabase).
		set.unreadable = append(set.unreadable, name)
	}
	if len(members) == 0 && len(set.unreadable) == 0 && set.unlisted == "" {
		return nil
	}
	for _, name := range members {
		file := migDir + "/" + name
		m := newMigration(file, name, in.Sources[file])
		// The CLI's name is the pattern's second group, as written;
		// .up.sql and .down.sql mean nothing to it.
		m.direction, m.description = "simple", supabaseName.FindStringSubmatch(name)[2]
		set.migrations = append(set.migrations, m)
	}
	for _, name := range set.skipped {
		set.diagnostic(nil, byteSpan{}, "info", "schema_migration_order", fmt.Sprintf("Supabase set %s: %s is the first entry and a legacy <timestamp>_init.sql older than 20211209000000, which the Supabase CLI skips", migDir, name))
	}
	if set.unlisted != "" {
		set.diagnostic(nil, byteSpan{}, "info", "schema_member_unreadable", fmt.Sprintf("Supabase set %s: the directory could not be listed (%s), so a member the scanner skipped cannot be reported; every table of the set is partial", migDir, set.unlisted))
	}
	for _, name := range set.unreadable {
		set.diagnostic(nil, byteSpan{}, "info", "schema_member_unreadable", fmt.Sprintf("Supabase set %s: member %s is not readable as UTF-8 text by the scanner (binary, ignored, a directory or a link); the Supabase CLI may apply it or fail on it, so whether it and the later migrations are applied is unknown, and every table of the set is partial", migDir, name))
	}
	set.supabaseRows = map[string]int{"SB-M1": 1}
	return set
}

// orderSupabase applies SB-M1's order: lexical, every member applied up
// to the first member whose outcome the pass cannot know. That is an
// unreadable member (the CLI may fail on it) or a second member with a
// version already used (its bookkeeping row is refused by the primary key
// of supabase_migrations.schema_migrations): from there on applied is
// unknown and the later files are not read.
func (set *migrationSet) orderSupabase() {
	sort.SliceStable(set.migrations, func(i, j int) bool { return set.migrations[i].name < set.migrations[j].name })
	firstUnread := ""
	for _, name := range set.unreadable {
		if firstUnread == "" || name < firstUnread {
			firstUnread = name
		}
	}
	seen := map[string]bool{}
	t := true
	order := 0
	stopped := false
	for _, m := range set.migrations {
		if !stopped && firstUnread != "" && firstUnread < m.name {
			stopped = true
		}
		if !stopped && seen[m.versionText] {
			stopped = true
			set.diagnostic(m, byteSpan{}, "warning", "schema_migration_order", fmt.Sprintf("version %s is used by an earlier migration; the Supabase CLI's bookkeeping row for this one is refused, so it and the later migrations are not known to be applied", m.versionText))
			set.stoppedAt = m.name
		}
		seen[m.versionText] = true
		order++
		m.order = order
		if stopped {
			m.applied = nil
			continue
		}
		m.applied = &t
	}
}

// supabaseText rewrites the reader's messages for a Supabase set: they
// name sqlx, whose behaviour the Supabase CLI shares where the reader
// uses it (one transaction per file, stop at the first failure).
var sqlxWord = regexp.MustCompile(`\bsqlx('s)?\b`)

func (set *migrationSet) toolText(s string) string {
	if set.tool != toolSupabase {
		return s
	}
	return sqlxWord.ReplaceAllStringFunc(s, func(w string) string {
		if strings.HasSuffix(w, "'s") {
			return "the Supabase CLI's"
		}
		return "the Supabase CLI"
	})
}

// supabaseTracker is the block a Supabase migration runs in: the CLI sends
// the file's statements and its bookkeeping INSERT as one pgx batch, which
// PostgreSQL runs in one implicit transaction block (an explicit BEGIN
// makes it an own block, COMMIT ends it), and the CLI does not read
// sqlx's -- no-transaction marker.
func supabaseTracker() *txTracker {
	return &txTracker{paths: []*txPath{{state: blockImplicit, noTx: true}}}
}

// supabaseBookkeeping is the CLI's bookkeeping check: a statement that
// drops or alters the supabase_migrations schema or its table may make
// the file's bookkeeping INSERT fail.
func (s *state) supabaseBookkeeping(toks []token) {
	c := &cursor{toks: toks}
	if !c.peekKw("drop") && !c.peekKw("alter") {
		return
	}
	for _, t := range toks {
		if (t.kind == tokIdent || t.kind == tokQuoted) && (t.value == "supabase_migrations" || t.value == "schema_migrations") {
			s.unknownFact("the Supabase CLI's INSERT INTO supabase_migrations.schema_migrations may be refused: the statement names its schema or table" + precondition("Q1.P13"))
			return
		}
	}
}

// platformReference applies SB-M2 to a foreign key whose referenced table
// the set does not hold: a key of a prelude table, written without
// columns or with its primary key column, from one column of the same
// type, resolves to that external object.
func (s *state) platformReference(fk *foreignKey, ref referenceDef) bool {
	if s.set.tool != toolSupabase || ref.table.schema == nil || len(fk.local) != 1 {
		return false
	}
	key := *ref.table.schema + "." + ref.table.name
	typ, ok := platformKeys[key]
	if !ok || ref.columns != nil && (len(ref.columns) != 1 || ref.columns[0] != "id") {
		return false
	}
	lc := fk.local[0]
	if !fkTypePair(lc, writtenType(lc), &column{name: "id", typ: typ}, typ) {
		return false
	}
	fk.external = key
	s.set.supabaseRows["SB-M2"]++
	return true
}

// rlsState is a table's row level security as the set leaves it (SB-M3).
// A nil field is unknown.
type rlsState struct {
	enabled, forced *bool
	// enabledAt and forcedAt are the table's count of opaque statements
	// that may change row level security (rlsOpaque) when the flag was
	// last set: such a statement after that may have changed it either
	// way (T024 review; SB-M9 data statements do not count).
	enabledAt, forcedAt int
}

// tablePolicy is one policy the set created on a table it holds, as
// written (SB-M3). Its table is the table that holds it, so it follows a
// rename of the table (T024 review), as pg_policies does.
type tablePolicy struct {
	name, command string
}

// rowSecurity applies SB-M3's ALTER TABLE words to a table the set holds:
// the flag the word sets, and nothing else.
func (s *state) rowSecurity(t *table, word string) {
	v := func(b bool) *bool { return &b }
	switch word {
	case "enable":
		t.rls.enabled, t.rls.enabledAt = v(true), t.rlsOpaque
	case "disable":
		t.rls.enabled, t.rls.enabledAt = v(false), t.rlsOpaque
	case "force":
		t.rls.forced, t.rls.forcedAt = v(true), t.rlsOpaque
	case "no_force":
		t.rls.forced, t.rls.forcedAt = v(false), t.rlsOpaque
	}
	s.set.supabaseRows["SB-M3"]++
}

// rlsWord reads which of the four ROW LEVEL SECURITY words an ALTER TABLE
// action is.
func rlsWord(c *cursor) string {
	switch {
	case c.peekKw("enable"):
		return "enable"
	case c.peekKw("disable"):
		return "disable"
	case c.peekKw("force"):
		return "force"
	case c.peekKw("no", "force"):
		return "no_force"
	}
	return ""
}

// policyList keeps t.policies through a CREATE POLICY, a DROP POLICY and
// an ALTER POLICY ... RENAME TO the pass read on a table it holds.
func (t *table) addPolicy(p tablePolicy) {
	t.policies = append(t.policies, p)
}

func (t *table) dropPolicy(name string) {
	for i, p := range t.policies {
		if p.name == name {
			t.policies = append(t.policies[:i], t.policies[i+1:]...)
			return
		}
	}
}

func (t *table) renamePolicy(name, to string) {
	for i, p := range t.policies {
		if p.name == name {
			t.policies[i].name = to
			return
		}
	}
}

// rlsAttributes are the SB-M3 table attributes: rls with each flag
// unknown (null) where a statement the pass did not read may have changed
// it, in either direction (T024 review): an opaque statement since the
// flag was set (O2: it may run any ALTER TABLE; an SB-M9 data statement,
// whose code is read and holds no such statement, does not count), a
// statement on the table
// that could not be read or code that alters it (rlsUnknown), or a member
// of the set that could not be read. Statements the pass read change the
// flags only through SB-M3's four words, so the table's other partial
// reasons (an unproven CHECK, a column a later migration adds) leave them
// known; a fact of a migration that may fail carries that migration's
// applied state, as every fact it writes (WF8). policies is null when the
// list is unknown; the policies of a partial table are those the set
// created, and others may exist. A policy's table is the table holding
// it, under its current name.
func rlsAttributes(set *migrationSet, t *table) (any, any) {
	setUnread := len(set.unreadable) > 0 || set.unlisted != "" || set.rlsEnded
	flag := func(b *bool, at int) any {
		if b == nil || t.rlsUnknown || setUnread || t.rlsOpaque > at {
			return nil
		}
		return *b
	}
	rls := map[string]any{"enabled": flag(t.rls.enabled, t.rls.enabledAt), "forced": flag(t.rls.forced, t.rls.forcedAt)}
	if t.policyNamesUnknown || t.policiesUnknown || set.rlsEnded {
		return rls, nil
	}
	list := []any{}
	for _, p := range t.policies {
		list = append(list, map[string]any{"name": p.name, "command": p.command, "table": t.q.String()})
	}
	return rls, list
}

// Row level security across data statements (row SB-M9, T024 review
// follow-up NX-F2; ADR 0077). In a set that defines functions or triggers,
// INSERT, UPDATE and DELETE are opaque (O1): a trigger they fire may run
// any statement. PostgreSQL 17 runs, for such a statement, only code the
// set can name: the row and statement triggers of the target table and of
// the tables its foreign key actions reach (RI triggers run UPDATE and
// DELETE there; trigger.c, ri_triggers.c), the rules of those tables
// (rewriteHandler.c), and the defaults, generation expressions, CHECK and
// domain constraints, index expressions and policy expressions of the rows
// it writes. Event triggers fire on DDL only (event_trigger.c), never on a
// data statement.
//
// The P12 inventory of a data statement (codeeffects.go) already reads a
// superset of that code: every trigger function of the set, every routine
// a statement of the set names outside its own definition, and the
// routines those call, closed over data changes. When that inventory is
// bounded (every body read; no EXECUTE, SET, RESET, set_config, transaction
// control, nested DO, body in another language, routine created by code)
// and holds no statement at all (no CREATE, ALTER, DROP, COMMENT, GRANT,
// REVOKE, SECURITY LABEL, TRUNCATE, REINDEX or CLUSTER, so no ALTER TABLE
// ... ROW LEVEL SECURITY and no policy statement), the statement cannot
// change any table's row level security flags or policies. That holds only
// while the pass has read every routine the set has: no earlier opaque
// statement other than such a data statement, and no unread statement
// (rlsBlocked). The statement stays opaque for every other fact.

// rlsInertData applies SB-M9 to an analysed top-level statement of a
// Supabase set: true when it is an INSERT, UPDATE or DELETE that is opaque
// only as a data change in a set that defines code (O1) and whose bounded
// inventory holds no statement.
func (s *state) rlsInertData(st statement, src []byte, a analysis, functions map[string]bool) bool {
	if s.set.tool != toolSupabase || s.rlsBlocked || s.rlsRuled || a.class != classOpaque || a.opaqueKind != "dml" || a.runsFunction != "" {
		return false
	}
	if len(st.tokens) == 0 || st.tokens[0].kind != tokIdent {
		return false
	}
	switch st.tokens[0].value {
	case "insert", "update", "delete":
	default:
		return false
	}
	targets, read := dataTargets(st.tokens, src)
	if !read || !s.rlsHeld(targets) {
		return false
	}
	inv := s.inventory(st, src, a, functions)
	if inv.unbounded != "" || len(inv.statements) > 0 || !s.rlsInventoryHeld(inv) {
		return false
	}
	s.set.supabaseRows["SB-M9"]++
	return true
}

// Data statements on relations the set does not hold (T024 review).
// A superuser's UPDATE pg_catalog.pg_class SET relrowsecurity = false, or
// DELETE FROM pg_catalog.pg_policy, changes row level security without
// any statement SB-M3 reads; an unqualified pg_class names the catalog
// (pg_catalog is searched first). A data statement whose target is not a
// table the set holds under a certain name may also reach a view's rules
// or a platform table's triggers the set does not show. SB-M9 therefore
// applies only when every target is held; any other data statement, top
// level or in code an O1 statement runs, and an unbounded inventory
// (which may run such a statement), make the rls flags and policies of
// every held table unknown, and no later data statement is SB-M9's (a
// catalog write may also change code). On the Supabase platform the
// postgres role is not a superuser, so a catalog write is refused there;
// the pass keeps the migration's may-fail as before.

// dataTargets reads the target relations of a data statement: INSERT
// INTO, UPDATE [ONLY], DELETE FROM [ONLY], MERGE INTO and COPY ... FROM.
// read is false for WITH and for a target the pass cannot read; a
// statement of another head has no targets.
func dataTargets(toks []token, src []byte) (targets []qname, read bool) {
	sc := &bodyScan{}
	sc.dataTarget(toks, src)
	if sc.writesAll || sc.deletesAll {
		return nil, false
	}
	return append(sc.writes, sc.deletes...), true
}

// dataHead reports whether a top-level statement is a data statement
// whose targets SB-M9 reads (rows.go's dataHeads and DELETE).
func dataHead(toks []token) bool {
	return len(toks) > 0 && toks[0].kind == tokIdent && (dataHeads[toks[0].value] || toks[0].value == "delete")
}

// rlsHeld reports whether every target is a table the set holds under a
// certain name. An unqualified name must resolve to one held table (an
// unqualified pg_ name never does: it names a pg_catalog relation when
// one exists, which the pass does not list). A qualified name must name a
// held table of that schema, or, in public, a table the set created
// unqualified (which then is that table or no relation at all: while
// SB-M9 holds, every relation the set made was read), and no other
// relation the pass records (a view may carry rules).
func (s *state) rlsHeld(targets []qname) bool {
	for _, q := range targets {
		if !s.rlsHeldTarget(q) {
			return false
		}
	}
	return true
}

func (s *state) rlsHeldTarget(q qname) bool {
	if q.schema == nil {
		return !strings.HasPrefix(q.name, "pg_") && s.writeTarget(q) != nil
	}
	if _, _, found := s.relationAt(q); found {
		return false
	}
	for _, t := range s.tables {
		if t.q.name != q.name || t.kindUnknown {
			continue
		}
		if t.q.schema != nil && *t.q.schema == *q.schema || t.q.schema == nil && *q.schema == "public" {
			return true
		}
	}
	return false
}

// rlsInventoryHeld reports whether every data target of a bounded
// inventory is held.
func (s *state) rlsInventoryHeld(inv inventory) bool {
	return !inv.writesAll && !inv.deletesAll && s.rlsHeld(inv.writes) && s.rlsHeld(inv.deletes)
}

// rlsForeignData applies B5 to a top-level statement of a Supabase set: a
// data statement whose targets are not all held (SB-M7's insert into
// storage.buckets excepted, under SB-V5) makes every held table's row
// level security unknown.
func (s *state) rlsForeignData(toks []token, src []byte) {
	if s.set.tool != toolSupabase || !dataHead(toks) {
		return
	}
	if s.rlsRuled {
		// A rule of the set may fire on this statement and run actions the
		// pass does not read (rlsRuleStatement).
		s.rlsForeign()
		return
	}
	if isBucketInsert(toks) {
		return
	}
	c := &cursor{toks: toks, src: src}
	if c.peekKw("copy") && !slices.ContainsFunc(toks, func(t token) bool { return t.kind == tokIdent && t.value == "from" }) {
		// COPY ... TO writes no table.
		return
	}
	if targets, read := dataTargets(toks, src); read && s.rlsHeld(targets) {
		return
	}
	s.rlsForeign()
}

// rlsRuleStatement applies the rule refusal of SB-M9 (stacked gate NX-S;
// ADR 0077) to a top-level statement of a Supabase set. PostgreSQL runs a
// rule's actions in place of, or beside, every INSERT, UPDATE or DELETE on
// its table (rewriteHandler.c), and an action may write a relation the set
// does not hold, pg_catalog.pg_class or pg_policy among them, which
// changes row level security without any statement SB-M3 reads. SB-M9
// does not read rule actions, so CREATE [OR REPLACE] RULE makes the rls
// flags and policies of every held table unknown, and every later data
// statement of the set does the same (rlsForeignData): a table created
// afterwards may be what an action writes. A rule that code of the set
// creates sets rlsRuled for the whole set (createsRule).
func (s *state) rlsRuleStatement(toks []token) {
	if s.set.tool != toolSupabase {
		return
	}
	if !createsRule(toks) {
		return
	}
	s.rlsRuled = true
	s.rlsForeign()
}

// createsRule reports CREATE [OR REPLACE] RULE. A rule created by code of
// the set (a DO block or routine body) counts from the start of the set,
// since the pass does not follow when that code runs.
func createsRule(toks []token) bool {
	c := &cursor{toks: toks}
	return c.peekKw("create", "rule") || c.peekKw("create", "or", "replace", "rule")
}

// rlsForeignCode applies B5 to the inventory of an O1 statement of a
// Supabase set.
func (s *state) rlsForeignCode(inv inventory) {
	if s.set.tool != toolSupabase {
		return
	}
	if inv.unbounded != "" {
		// TOP-1 of 2026-11-18: code the pass cannot read (a dynamic EXECUTE,
		// a body it cannot read, a routine created by code) may create a
		// rule, a trigger or an event trigger.
		s.rlsEnd()
		return
	}
	if !s.rlsInventoryHeld(inv) {
		s.rlsForeign()
	}
}

// rlsEnd ends SB-M9 for the rest of a Supabase set (TOP-1 of 2026-11-18): a
// statement ran code the pass cannot read, or created an event trigger,
// or the rest of a file could not be lexed. Such code may create a rule
// (whose actions run with every later data statement on its table), a
// trigger, or an event trigger (whose function runs with every later DDL
// statement, ALTER TABLE ... ROW LEVEL SECURITY and CREATE POLICY among
// them), and any of these may change row level security without a
// statement SB-M3 reads. Unlike a rule the pass sees (rlsRuleStatement),
// no later statement is safe, so the rls flags and policies of every
// table, held now or created later, stay unknown to the end of the set
// (migrationSet.rlsEnded, read by rlsAttributes), and no later data
// statement is SB-M9's.
func (s *state) rlsEnd() {
	s.rlsRuled = true
	s.set.rlsEnded = true
	s.rlsForeign()
}

// rlsForeign makes the row level security flags and policies of every
// held table unknown: a statement the pass does not follow may have
// changed them.
func (s *state) rlsForeign() {
	s.rlsBlocked = true
	for _, t := range s.tables {
		t.rlsOpaque++
		t.policiesUnknown = true
	}
}

// Column types created by a platform extension (row SB-M10, NX-F2; ADR
// 0077). SB-M6 makes CREATE EXTENSION x [WITH] SCHEMA s a known effect for
// the platform extensions; the types its install script creates are then
// types in s, and a column may use them. A written column type resolves to
// such a type as Postgres resolves a type name (namespace.c
// TypenameGetTypidExtended, parse_type.c LookupTypeNameExtended): a
// qualified name only in its schema; an unqualified one by the session's
// search_path, pg_catalog (and the temporary schema) first. The pass
// proves the unqualified case only when the path is known, the
// extension's schema is on it (E4's default value is the Supabase
// platform's "$user", public, extensions, assumption SB-V2) and no schema
// before it can hold a type of the name: the set creates no type or
// relation of the name anywhere and its code may create none, and
// pg_catalog has none. Every other form keeps M2.P1's may fail.
//
// Type modifiers follow each type's typmod_in, read from the install
// scripts and checked live on PostgreSQL 17.12 with the PGDG builds of
// pgvector 0.8.7 and pgsql-http 1.7: vector, halfvec and sparsevec take
// exactly one integer from 1 to their maximum dimensions (16000, 16000,
// 1000000000; otherwise "invalid type modifier" or "dimensions for type
// ... must be at least 1 / cannot exceed ...", 22023); the http composite
// types and domains take none ("type modifier is not allowed for type",
// 42601). A modifier that is not an integer constant, and one on an array
// type written by its internal name (_vector), is not proven.

// platformType is one type a platform extension's install script creates.
type platformType struct {
	// maxDims is the largest modifier the type's typmod_in takes; 0 for a
	// type that takes no modifier.
	maxDims int
	// array marks an array type written by its internal name.
	array bool
}

// supabaseExtensionTypes are the types of each SB-M6 extension, by name
// (vector--0.8.7.sql CREATE TYPE vector, halfvec, sparsevec; http--1.7.sql
// CREATE DOMAIN http_method, content_type and CREATE TYPE http_header,
// http_response, http_request), with the array types Postgres creates for
// each. Every name is also in supabaseExtensionObjects.
var supabaseExtensionTypes = map[string]map[string]platformType{
	"vector": {
		"vector": {maxDims: 16000}, "halfvec": {maxDims: 16000}, "sparsevec": {maxDims: 1000000000},
		"_vector": {array: true}, "_halfvec": {array: true}, "_sparsevec": {array: true},
	},
	"http": {
		"http_method": {}, "content_type": {}, "http_header": {}, "http_response": {}, "http_request": {},
		"_http_method": {array: true}, "_content_type": {array: true}, "_http_header": {array: true},
		"_http_response": {array: true}, "_http_request": {array: true},
	},
}

// heldPlatformType is a type an SB-M6 extension created in a schema.
type heldPlatformType struct {
	platformType
	ext, schema string
	// opaqueAt is the opaque count when the extension was created: an
	// opaque statement since then may have dropped or moved it.
	opaqueAt int
	// gone is set once a statement may have altered or dropped it.
	gone bool
}

// recordPlatformTypes records the types of an SB-M6 extension created in
// schema. A second creation of the extension (IF NOT EXISTS may skip it,
// leaving the types in the first schema) forgets them.
func (s *state) recordPlatformTypes(ext, schema string) {
	if s.platformTypes == nil {
		s.platformTypes = map[string]*heldPlatformType{}
	}
	for name, pt := range supabaseExtensionTypes[ext] {
		if prev := s.platformTypes[name]; prev != nil {
			prev.gone = true
			continue
		}
		s.platformTypes[name] = &heldPlatformType{platformType: pt, ext: ext, schema: schema, opaqueAt: s.opaqueSeen}
	}
}

// platformTypeStatement forgets a recorded type that a top-level ALTER
// or DROP TYPE or DOMAIN names (ALTER TYPE ... RENAME or SET SCHEMA of an
// extension member is allowed; a DROP of one is refused or cascades), and
// every type of a schema that ALTER or DROP SCHEMA names.
func (s *state) platformTypeStatement(toks []token) {
	if len(s.platformTypes) == 0 {
		return
	}
	c := &cursor{toks: toks}
	typeWords := c.peekKw("alter", "type") || c.peekKw("alter", "domain") || c.peekKw("drop", "type") || c.peekKw("drop", "domain")
	schemaWords := c.peekKw("alter", "schema") || c.peekKw("drop", "schema")
	if !typeWords && !schemaWords {
		return
	}
	for _, t := range toks[2:] {
		if t.kind != tokIdent && t.kind != tokQuoted {
			continue
		}
		for name, pt := range s.platformTypes {
			if typeWords && t.value == name || schemaWords && t.value == pt.schema {
				pt.gone = true
			}
		}
	}
}

// platformColumnType applies SB-M10 to a written column type: the held
// platform type it resolves to, or nil when that is not proven.
func (s *state) platformColumnType(typ string) *heldPlatformType {
	if s.set == nil || s.set.tool != toolSupabase || len(s.platformTypes) == 0 {
		return nil
	}
	canon, ok := canonicalType(typ)
	if !ok {
		return nil
	}
	base := canon
	for strings.HasSuffix(base, "[]") {
		base = strings.TrimSuffix(base, "[]")
	}
	if i := strings.Index(base, "("); i >= 0 {
		base = base[:i]
	}
	schema, name, qualified := strings.Cut(base, ".")
	if !qualified {
		name, schema = schema, ""
	}
	pt := s.platformTypes[name]
	if pt == nil || pt.gone || pt.opaqueAt != s.opaqueSeen {
		return nil
	}
	if s.names.creates(name, kindType|kindRelation) || s.code.may[name]&(kindType|kindRelation) != 0 || s.code.gone[name] || s.holds(name, kindType|kindRelation) {
		return nil
	}
	if qualified {
		if schema != pt.schema {
			return nil
		}
		return pt
	}
	if _, builtin := pgTypes[name]; builtin {
		return nil
	}
	p := s.effectivePath()
	if !p.known {
		return nil
	}
	for _, e := range p.entries {
		entries := []string{e}
		if e == defaultEntry {
			// E4's value on the Supabase platform (SB-V2).
			entries = []string{"$user", "public", "extensions"}
		}
		for _, x := range entries {
			if x == pt.schema {
				return pt
			}
		}
	}
	return nil
}

// typmod applies the type's typmod_in to the modifiers of a written type
// (canonical text): the verdict as typmodCheck gives it.
func (pt *heldPlatformType) typmod(typ string) (typmodVerdict, string) {
	canon, ok := canonicalType(typ)
	if !ok {
		return typmodUnknown, "the type is not read"
	}
	open := strings.Index(canon, "(")
	if open < 0 {
		return typmodOK, ""
	}
	closeAt := strings.Index(canon, ")")
	if closeAt < open {
		return typmodUnknown, "the modifiers are not read"
	}
	var mods []int
	for _, f := range strings.Split(canon[open+1:closeAt], ",") {
		n, err := strconv.Atoi(f)
		if err != nil {
			return typmodUnknown, "a modifier is not an integer constant"
		}
		mods = append(mods, n)
	}
	name := bareName(canon[:open])
	switch {
	case pt.array:
		return typmodUnknown, fmt.Sprintf("the modifiers of %s are not proven", name)
	case pt.maxDims == 0:
		return typmodRefused, fmt.Sprintf("type modifier is not allowed for type %s", name)
	case len(mods) != 1:
		return typmodRefused, fmt.Sprintf("invalid type modifier for type %s", name)
	case mods[0] < 1:
		return typmodRefused, fmt.Sprintf("dimensions for type %s must be at least 1", name)
	case mods[0] > pt.maxDims:
		return typmodRefused, fmt.Sprintf("dimensions for type %s cannot exceed %d", name, pt.maxDims)
	}
	return typmodOK, ""
}

// columnTypeProven is M2.P1 for a column's type: typeProven, or a platform
// extension's type (SB-M10).
func (s *state) columnTypeProven(typ string) string {
	why := s.typeProven(typ)
	if why != "" && s.platformColumnType(typ) != nil {
		return ""
	}
	return why
}
