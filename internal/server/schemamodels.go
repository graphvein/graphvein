package server

import (
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

// This file holds the rules T018f adds to the schema endpoints (ADR 0055,
// docs/cli/serve.md): model groups, the model/migration name overlap, the
// opaque statements behind a table's opaque_after, rests_on, the unread
// tools and sources_read. Like schemadata.go, every rule falls back to
// unknown (null) where the records do not prove a value.

// Source names of groups.
const (
	sourceMigrations = "migrations"
	sourceModels     = "models"
	// sourceTableAccess is the catalogue source of the code-to-table
	// assumptions (TA-V ids); no group has it.
	sourceTableAccess = "table_access"
)

// firstModelVersion is the first aggregate extractor version that runs the
// model linker (T018c2, ADR 0054): from it on an index proves that both
// passes ran.
const firstModelVersion = 29

// modelRecord reports a model-defined schema record (ADR 0054).
func modelRecord(n contract.Node) bool { return n.Attributes["source"] == "model" }

// modelCompleteness is the completeness of a model table: complete for
// state complete, partial for partial, unknown otherwise. Models have no
// opaque statements, so complete_in_source never applies.
func modelCompleteness(attrs map[string]any) string {
	switch attrs["state"] {
	case "complete":
		return completenessComplete
	case "partial":
		return completenessPartial
	}
	return completenessUnknown
}

// Model column default states (T018f): server for a server default; python
// for a Python-side default (the database has none); client for a value
// the Prisma client generates on insert (T018g1: uuid(), cuid(), ...; the
// database has none); none for a proven absence on a complete table;
// unknown otherwise.
const (
	defaultServer = "server"
	defaultPython = "python"
	defaultClient = "client"
)

// modelDefaultState is the one rule for a model column's default.
func modelDefaultState(attrs map[string]any, tableCompleteness string) string {
	server, def := attrs["server_default"], attrs["default"]
	switch {
	case server == true:
		return defaultServer
	case server != false:
		return defaultUnknown
	case def == "scalar" || def == "callable":
		return defaultPython
	case def == "client":
		return defaultClient
	case def == "absent" && tableCompleteness == completenessComplete:
		return defaultNone
	}
	return defaultUnknown
}

// tableCompleteness applies the completeness rule of the table's source.
func tableCompleteness(n contract.Node) string {
	if modelRecord(n) {
		return modelCompleteness(n.Attributes)
	}
	return completeness(n.Attributes)
}

// columnReference is one reference entry of a column (verbatim, as stored)
// and whether it links to a stored column.
type columnReference struct {
	Entry  any
	Linked bool
	// External is a migration reference to a platform object outside the
	// set (status "external", T024c1 SB-M2): neither linked nor unresolved.
	External bool
}

// columnReferences applies the reference rule of the column's source, so
// that every reader of a column's foreign keys counts a model reference the
// linker could not resolve as unresolved (T018f Revision 2, A1) instead of
// reading only the migration attributes and seeing none.
//
// Migration columns: the references value (when not null) and each element
// of additional_references; linked only for status "linked". Model columns
// (ADR 0054): each element of foreign_keys; linked only for status
// "resolved" (an "ambiguous" or "unresolved" target is unresolved). A
// value of another JSON type counts as one unresolved entry, so it is
// reported rather than dropped.
func columnReferences(col contract.Node) []columnReference {
	var out []columnReference
	if !modelRecord(col) {
		for _, e := range referenceEntries(col.Attributes) {
			m, _ := e.(map[string]any)
			out = append(out, columnReference{Entry: e, Linked: linkedEntry(e), External: m != nil && m["status"] == "external"})
		}
		return out
	}
	v, ok := col.Attributes["foreign_keys"]
	if !ok || v == nil {
		return nil
	}
	items, isList := v.([]any)
	if !isList {
		return []columnReference{{Entry: v}}
	}
	for _, e := range items {
		m, isObject := e.(map[string]any)
		out = append(out, columnReference{Entry: e, Linked: isObject && m["status"] == "resolved"})
	}
	return out
}

// columnDefaultState applies the default rule of the column's source.
func columnDefaultState(col contract.Node, tableCompleteness string) string {
	if modelRecord(col) {
		return modelDefaultState(col.Attributes, tableCompleteness)
	}
	return defaultState(col.Attributes, tableCompleteness)
}

// sourcesRead lists the sources whose pass the last run's version proves
// ran: migrations from firstSchemaVersion, models too from
// firstModelVersion; [] for a graphvein version before the schema pass
// (found is unknown then); nil when not indexed or the version is not
// graphvein's.
func sourcesRead(last *lastRun) []string {
	if last == nil || last.Indexer.Name != "graphvein" {
		return nil
	}
	m := extractorsVersion.FindStringSubmatch(last.Indexer.Version)
	if m == nil {
		return nil
	}
	n, err := strconv.Atoi(m[1])
	switch {
	case err != nil:
		return nil
	case n >= firstModelVersion:
		return []string{sourceMigrations, sourceModels}
	case n >= firstSchemaVersion:
		return []string{sourceMigrations}
	}
	return []string{}
}

// schemaUnreadTool is a migration tool found among the stored paths that
// no pass reads (T018f 1.3).
type schemaUnreadTool struct {
	Tool   string  `json:"tool"`
	Path   *string `json:"path"`
	Marker *string `json:"marker"`
	Files  *int    `json:"files"`
}

// unreadTools applies the Alembic marker rule to the stored file paths: a
// script directory D has D/env.py and D/script.py.mako or a D/versions/*.py
// file; its marker is the nearest alembic.ini in D or an ancestor; files
// counts D/versions/*.py without __init__.py. An alembic.ini that is no
// directory's marker is listed with a null path. Prisma Migrate's
// directories follow prismaMigrations; named are the directories Prisma
// configs name (namedMigrations). drizzle-kit's migration folders follow
// drizzleMigrations; drizzle are the folders Drizzle configs name
// (namedDrizzleMigrations). sequelize-cli and umzug folders follow
// sequelizeMigrations; sequelize are the folders the Sequelize linker
// marks. The list names what the rules found and nothing else.
func unreadTools(files []string, named, drizzle, sequelize, diesel map[string]bool) []schemaUnreadTool {
	stored := map[string]bool{}
	for _, f := range files {
		stored[f] = true
	}
	join := func(dir, name string) string {
		if dir == "." {
			return name
		}
		return dir + "/" + name
	}
	versions := map[string]int{}
	hasVersion := map[string]bool{}
	for _, f := range files {
		dir, base := path.Split(f)
		dir = strings.TrimSuffix(dir, "/")
		if path.Base(dir) != "versions" || !strings.HasSuffix(base, ".py") {
			continue
		}
		parent := path.Dir(dir)
		hasVersion[parent] = true
		if base != "__init__.py" {
			versions[parent]++
		}
	}
	out := []schemaUnreadTool{}
	marked := map[string]bool{}
	for _, f := range files {
		if path.Base(f) != "env.py" {
			continue
		}
		dir := path.Dir(f)
		if !stored[join(dir, "script.py.mako")] && !hasVersion[dir] {
			continue
		}
		entry := schemaUnreadTool{Tool: "alembic", Path: &dir}
		for d := dir; ; d = path.Dir(d) {
			if ini := join(d, "alembic.ini"); stored[ini] {
				entry.Marker = &ini
				marked[ini] = true
				break
			}
			if d == "." {
				break
			}
		}
		n := versions[dir]
		entry.Files = &n
		out = append(out, entry)
	}
	for _, f := range files {
		if path.Base(f) == "alembic.ini" && !marked[f] {
			ini := f
			out = append(out, schemaUnreadTool{Tool: "alembic", Marker: &ini})
		}
	}
	out = append(out, prismaMigrations(files, stored, named)...)
	out = append(out, drizzleMigrations(files, stored, drizzle)...)
	out = append(out, sequelizeMigrations(files, sequelize)...)
	out = append(out, dieselMigrations(files, diesel)...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !equalOptional(a.Path, b.Path) {
			return lessSetKey(a.Path, b.Path)
		}
		return lessSetKey(a.Marker, b.Marker)
	})
	return out
}

// prismaMigrations applies the Prisma Migrate rule (T018g1 review,
// decision 2026-11-09): a directory D/migrations beside a
// Prisma schema (D holds a stored .prisma file), or a directory a Prisma 7
// config names in a literal migrations.path (named; T018g1 G1G), that
// contains migration_lock.toml or a migration directory's migration.sql
// (<dir>/<name>/migration.sql) is a prisma-migrate tool no pass reads (the
// SQL pass reads sqlx sets only). Path is the directory, Marker its
// migration_lock.toml when stored, Files the migration.sql count. A
// directory beside a schema is listed even when a config names another
// one: it is still found on disk, and the list never hides migrations.
func prismaMigrations(files []string, stored, named map[string]bool) []schemaUnreadTool {
	schemaDirs := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, ".prisma") {
			schemaDirs[path.Dir(f)] = true
		}
	}
	accept := func(dir string) bool {
		return named[dir] || path.Base(dir) == "migrations" && schemaDirs[path.Dir(dir)]
	}
	counts := map[string]int{}
	var dirs []string
	note := func(dir string) {
		if _, seen := counts[dir]; !seen {
			counts[dir] = 0
			dirs = append(dirs, dir)
		}
	}
	for _, f := range files {
		switch path.Base(f) {
		case "migration_lock.toml":
			if dir := path.Dir(f); accept(dir) {
				note(dir)
			}
		case "migration.sql":
			parent := path.Dir(f)
			if dir := path.Dir(parent); parent != "." && accept(dir) {
				note(dir)
				counts[dir]++
			}
		}
	}
	var out []schemaUnreadTool
	for _, dir := range dirs {
		entry := schemaUnreadTool{Tool: "prisma-migrate", Path: &dir}
		if lock := dir + "/migration_lock.toml"; stored[lock] {
			entry.Marker = &lock
		}
		n := counts[dir]
		entry.Files = &n
		out = append(out, entry)
	}
	return out
}

// drizzleMigrations applies the drizzle-kit rule
// (decision 2026-11-10): a folder a Drizzle config names as its migrations
// folder (the literal `out`, default "drizzle", resolved by the Drizzle
// linker; named) that holds meta/_journal.json or a .sql file directly
// in it is a drizzle-kit tool no pass reads. Path is the folder, Marker
// its meta/_journal.json when stored, Files the .sql count.
func drizzleMigrations(files []string, stored, named map[string]bool) []schemaUnreadTool {
	counts := map[string]int{}
	for _, f := range files {
		if dir := path.Dir(f); named[dir] && strings.HasSuffix(f, ".sql") {
			counts[dir]++
		}
	}
	dirs := make([]string, 0, len(named))
	for dir := range named {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	var out []schemaUnreadTool
	for _, dir := range dirs {
		entry := schemaUnreadTool{Tool: "drizzle-kit", Path: &dir}
		if journal := dir + "/meta/_journal.json"; stored[journal] {
			entry.Marker = &journal
		} else if counts[dir] == 0 {
			continue
		}
		n := counts[dir]
		entry.Files = &n
		out = append(out, entry)
	}
	return out
}

// sequelizeMigrations applies the Sequelize rule (T018g3, row
// SQ-U1): a folder the Sequelize linker marks as a package's migrations
// folder (sequelize-cli's or umzug's default migrations/) that holds a
// script file at any depth is a sequelize-cli or umzug tool no pass
// reads. Path is the folder, Files the script count; there is no marker
// file.
func sequelizeMigrations(files []string, named map[string]bool) []schemaUnreadTool {
	counts := map[string]int{}
	for _, f := range files {
		if dir := sequelizeDir(f, named); dir != "" {
			counts[dir]++
		}
	}
	dirs := make([]string, 0, len(counts))
	for dir := range counts {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	var out []schemaUnreadTool
	for _, dir := range dirs {
		dir := dir
		n := counts[dir]
		out = append(out, schemaUnreadTool{Tool: "sequelize-migrations", Path: &dir, Files: &n})
	}
	return out
}

// dieselMigrations applies the Diesel rule (T018g4, row DI-U1,
// Revision 1 addendum OP1): a directory the Diesel linker marks as a
// diesel.toml's migrations directory (`[migrations_directory] dir`, else
// migrations beside it) that holds a migration directory with an up.sql
// is a Diesel migrations tool no pass reads. Path is the directory, Files
// the count of its migration directories (direct subdirectories with an
// up.sql, dot entries skipped: the linker's own count,
// internal/extract/models/diesel MigrationDirs); there is no marker file.
func dieselMigrations(files []string, named map[string]bool) []schemaUnreadTool {
	counts := map[string]int{}
	for _, f := range files {
		if dir := dieselDir(f, named); dir != "" {
			counts[dir]++
		}
	}
	dirs := make([]string, 0, len(counts))
	for dir := range counts {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	var out []schemaUnreadTool
	for _, dir := range dirs {
		dir := dir
		n := counts[dir]
		out = append(out, schemaUnreadTool{Tool: "diesel-migrations", Path: &dir, Files: &n})
	}
	return out
}

// dieselDir returns the named directory D of a stored D/<name>/up.sql
// whose <name> is not a dot entry, or "".
func dieselDir(f string, named map[string]bool) string {
	if len(named) == 0 || path.Base(f) != "up.sql" {
		return ""
	}
	migration := path.Dir(f)
	if migration == "." || strings.HasPrefix(path.Base(migration), ".") {
		return ""
	}
	if dir := path.Dir(migration); named[dir] {
		return dir
	}
	return ""
}

// sequelizeDir returns the named folder a stored script file lies under
// (the nearest), or "" (the Sequelize linker's own count rule:
// internal/extract/models/sequelize scriptFile).
func sequelizeDir(f string, named map[string]bool) string {
	if len(named) == 0 {
		return ""
	}
	switch path.Ext(f) {
	case ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs":
	default:
		return ""
	}
	for dir := path.Dir(f); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if named[dir] {
			return dir
		}
	}
	return ""
}

// drizzlePath reports a stored path the drizzle-kit rule reads: a .sql
// file directly in a named folder, or its meta/_journal.json.
func drizzlePath(f string, named map[string]bool) bool {
	dir := path.Dir(f)
	switch {
	case strings.HasSuffix(f, ".sql"):
		return named[dir]
	case path.Base(f) == "_journal.json" && path.Base(dir) == "meta":
		return named[path.Dir(dir)]
	}
	return false
}

// prismaMigrationsMarker is the Prisma linker's schema_model_migrations
// message after "<config>: "; the directory a config's literal
// migrations.path names follows it (internal/extract/models/prisma).
const (
	codePrismaMigrations   = "schema_model_migrations"
	prismaMigrationsMarker = "Prisma Migrate migrations directory (not read): "
	// drizzleMigrationsMarker is the Drizzle linker's message part before
	// a config's migrations folder (internal/extract/models/drizzle), with
	// the same code.
	drizzleMigrationsMarker = "drizzle-kit migrations folder (not read): "
	// sequelizeMigrationsMarker is the Sequelize linker's message part
	// before a package's migrations folder
	// (internal/extract/models/sequelize), with the same code.
	sequelizeMigrationsMarker = "sequelize-cli or umzug migrations folder (not read): "
	// dieselMigrationsMarker is the Diesel linker's message part before a
	// diesel.toml's migrations directory (internal/extract/models/diesel
	// MigrationsMarker), with the same code.
	dieselMigrationsMarker = "Diesel migrations directory (not read): "
)

// gormV1MarkerText is the part of every GO-U1 marker the store filters on
// before gormV1Marker decides.
const gormV1MarkerText = " import (GORM v1): found, not read"

// gormV1Marker matches the GORM linker's row GO-U1 marker
// (internal/extract/models/gorm unreadV1): "<file>:<line>:
// github.com/jinzhu/gorm[/...] import (GORM v1): found, not read".
var gormV1Marker = regexp.MustCompile(`^(.+):[0-9]+: github\.com/jinzhu/gorm(/[^ ]*)? import \(GORM v1\): found, not read$`)

// gormV1Files reads the stored Go files the GORM linker marks as importing
// GORM v1 (T018g5, row GO-U1, Revision 1 addendum 2 P2): a
// schema_model_unread diagnostic on the file whose message names the file
// itself. A message of another shape names nothing.
func gormV1Files(diagnostics []contract.Diagnostic, hashes map[string]string) map[string]bool {
	files := map[string]bool{}
	for _, d := range diagnostics {
		if _, ok := hashes[d.File]; !ok || d.Code != codeModelUnread {
			continue
		}
		if m := gormV1Marker.FindStringSubmatch(d.Message); m != nil && m[1] == d.File {
			files[d.File] = true
		}
	}
	return files
}

// withGormV1 adds the GORM v1 rule to an unread_tools list: one gorm-v1
// entry per Go package directory holding a marked file (Path the
// directory, Files the count of its marked files, no marker file: GORM v1
// has no migrations folder or config of its own), kept in the list's
// order.
func withGormV1(out []schemaUnreadTool, files map[string]bool) []schemaUnreadTool {
	if len(files) == 0 {
		return out
	}
	counts := map[string]int{}
	for f := range files {
		counts[path.Dir(f)]++
	}
	dirs := make([]string, 0, len(counts))
	for dir := range counts {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		dir := dir
		n := counts[dir]
		out = append(out, schemaUnreadTool{Tool: "gorm-v1", Path: &dir, Files: &n})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !equalOptional(a.Path, b.Path) {
			return lessSetKey(a.Path, b.Path)
		}
		return lessSetKey(a.Marker, b.Marker)
	})
	return out
}

// typeormMigrationsMarker is the TypeORM linker's
// schema_model_migrations message part before a data source's migrations
// folder (internal/extract/models/typeorm, row TO-U1: the directory before
// the first wildcard of a migrations pattern), with the same code.
const typeormMigrationsMarker = "typeorm migrations (not read): "

// withTypeORM adds the TypeORM rule to an unread_tools list (T018g6,
// Revision 3 addendum 2 OB): one typeorm-migrations entry per folder
// a data source's migrations option names that holds stored script files
// (Path the folder, Files the script files at any depth under it, nearest
// named folder first, as the Sequelize rule counts), kept in the list's
// order. A folder without stored scripts (a build-output pattern such as
// dist/migrations) adds no entry.
func withTypeORM(out []schemaUnreadTool, files []string, named map[string]bool) []schemaUnreadTool {
	if len(named) == 0 {
		return out
	}
	counts := map[string]int{}
	for _, f := range files {
		if dir := sequelizeDir(f, named); dir != "" {
			counts[dir]++
		}
	}
	if len(counts) == 0 {
		return out
	}
	dirs := make([]string, 0, len(counts))
	for dir := range counts {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		dir := dir
		n := counts[dir]
		out = append(out, schemaUnreadTool{Tool: "typeorm-migrations", Path: &dir, Files: &n})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !equalOptional(a.Path, b.Path) {
			return lessSetKey(a.Path, b.Path)
		}
		return lessSetKey(a.Marker, b.Marker)
	})
	return out
}

// namedMigrations reads the directories of the stored configs'
// schema_model_migrations markers with marker (prismaMigrationsMarker or
// drizzleMigrationsMarker). A message of another shape, or a directory
// that is not a clean repository-relative path, names nothing (the
// default rule still applies).
func namedMigrations(diagnostics []contract.Diagnostic, hashes map[string]string, marker string) map[string]bool {
	named := map[string]bool{}
	for _, d := range diagnostics {
		if _, ok := hashes[d.File]; !ok || d.Code != codePrismaMigrations {
			continue
		}
		dir, ok := strings.CutPrefix(d.Message, d.File+": "+marker)
		if !ok || dir == "" || dir == "." || path.Clean(dir) != dir || path.IsAbs(dir) || dir == ".." || strings.HasPrefix(dir, "../") {
			continue
		}
		named[dir] = true
	}
	return named
}

// alembicPath reports a path unreadTools reads: env.py, script.py.mako,
// alembic.ini or a .py file directly in a versions directory, and for the
// Prisma rule a .prisma file, migration_lock.toml or migration.sql. Every
// other stored path leaves its result unchanged.
func alembicPath(f string) bool {
	dir, base := path.Split(f)
	switch base {
	case "env.py", "script.py.mako", "alembic.ini", "migration_lock.toml", "migration.sql":
		return true
	}
	if strings.HasSuffix(base, ".prisma") {
		return true
	}
	return strings.HasSuffix(base, ".py") && path.Base(strings.TrimSuffix(dir, "/")) == "versions"
}

func equalOptional(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// schemaModelFields are the fields only a model group carries.
type schemaModelFields struct {
	// Pins are the library pins every table states; null with
	// PinsConflict when they differ or one is missing.
	Pins         any      `json:"pins"`
	PinsConflict bool     `json:"pins_conflict"`
	Assumptions  []string `json:"assumptions"`
	// Dialects are the run-time dialects every table of a group whose
	// dialect is chosen at run time lists (ADR 0065: GORM's gotify, TypeORM's
	// seerr); its columns may then carry per-dialect facts (by_dialect) and
	// claims naming their dialect. Omitted for every other group, so their
	// responses are unchanged; tables that disagree leave it out and add
	// "dialects" to conflicts.
	Dialects []string `json:"dialects,omitempty"`
	// Files are the files that hold a model table or a model diagnostic
	// with a count (unread classes, Core tables).
	Files []schemaModelFile `json:"files"`
}

type schemaModelFile struct {
	File       string `json:"file"`
	Tables     int    `json:"tables"`
	Unread     *int64 `json:"unread"`
	CoreTables *int64 `json:"core_tables"`
}

// Model diagnostic codes the group rules read.
const (
	codeModelUnread        = "schema_model_unread"
	codeModelCoreTables    = "schema_model_core_tables"
	codeModelImportFailure = "schema_model_import_failure"
	codeModelNotClaimed    = "schema_model_not_claimed"
)

// Table list reasons of a model group for Python files the index did not
// read (T018f Revision 2, B1).
const (
	reasonPythonUnread  = "python_unread"
	reasonPythonScanGap = "python_scan_gap"
)

// fileNotReadCodes are the diagnostics that say a file was found but its
// content was not read or not extracted: too large, failed to parse, an
// extractor failed, the scanner could not read it, or the scanner skipped
// it as binary, not UTF-8 or not a regular file (row CR-34). The schema
// views' Python rule and the couldn't-read page (row CR-19) share this one
// set.
var fileNotReadCodes = map[string]bool{
	"file_too_large":   true,
	"parse_failed":     true,
	"extract_failed":   true,
	"file_read":        true,
	"file_binary":      true,
	"file_not_utf8":    true,
	"file_not_regular": true,
}

// pythonNotRead reports a diagnostic saying a .py file was not read.
func pythonNotRead(d contract.Diagnostic) bool {
	return fileNotReadCodes[d.Code] && strings.HasSuffix(d.File, ".py")
}

// pythonScanGap reports a scanner diagnostic that may hide a .py file: a
// directory that could not be listed, a symlink not followed (it may point
// at a directory or a .py file), a skipped path, an unreadable ignore file
// (its rules are unknown), or an unreadable, non-UTF-8 or non-regular file
// whose name does not say it is not Python (no extension, or no path at
// all).
func pythonScanGap(d contract.Diagnostic) bool {
	switch d.Code {
	case "scan_read", "symlink", "invalid_path", "ignore_read":
		return true
	case "file_read", "file_not_utf8", "file_not_regular":
		// A binary file (file_binary) is never Python source: Python
		// refuses a NUL byte, so only its .py name matters (pythonNotRead).
		return d.File == "" || path.Ext(d.File) == "" || strings.HasSuffix(d.File, ".py")
	}
	return false
}

// pythonListReasons returns the B1 reasons that hold for the whole
// repository, whatever its model groups: python_unread when a stored .py
// file was not read (diagnostics are the stored .py files' diagnostics),
// python_scan_gap when a scanner gap (unfiled) may hide one. Sorted; []
// when none holds.
func pythonListReasons(diagnostics, unfiled []contract.Diagnostic) []string {
	reasons := map[string]bool{}
	for _, d := range diagnostics {
		if pythonNotRead(d) {
			reasons[reasonPythonUnread] = true
		}
	}
	for _, d := range unfiled {
		if pythonNotRead(d) {
			reasons[reasonPythonUnread] = true
		} else if pythonScanGap(d) {
			reasons[reasonPythonScanGap] = true
		}
	}
	return sortedKeys(reasons)
}

// modelListsComplete reports whether every model table of the repository
// is listed: the model pass ran (read holds models), no B1 reason holds
// and every model group's table_list (groups are the model tables by
// group) is complete. Only then is a migration table's overlap list
// proven; otherwise it is null (T018f Revision 2, F6 decision a: the
// mirror of C6).
func modelListsComplete(read []string, groups [][]contract.Node, diagnostics, unfiled []contract.Diagnostic) bool {
	if !slices.Contains(read, sourceModels) || len(pythonListReasons(diagnostics, unfiled)) > 0 {
		return false
	}
	for _, tables := range groups {
		s := schemaSet{DiagnosticCounts: map[string]int{}}
		modelGroupState(&s, tables, diagnostics, unfiled, read)
		if s.TableList == nil || *s.TableList != tableListComplete {
			return false
		}
	}
	return true
}

// modelGroupsOf splits the model tables among tables by their group (set
// key), as /sets groups them.
func modelGroupsOf(tables []contract.Node) [][]contract.Node {
	byKey := map[string][]contract.Node{}
	keys := []string{}
	for _, t := range tables {
		if !modelRecord(t) || groupKey(t) == nil {
			continue
		}
		k := keyString(groupKey(t))
		if byKey[k] == nil {
			keys = append(keys, k)
		}
		byKey[k] = append(byKey[k], t)
	}
	sort.Strings(keys)
	out := [][]contract.Node{}
	for _, k := range keys {
		out = append(out, byKey[k])
	}
	return out
}

// modelListMarker is the suffix the TypeScript model linkers (Drizzle,
// internal/extract/models/drizzle ListMarker) put on a diagnostic about a
// declaration that may be a table of model group set but is not claimed
// there (T018g2 review): the diagnostic belongs to that group's
// table_list whatever file it is on, because a module without a model
// payload or a table creator's file need not hold a claimed table of the
// group.
func modelListMarker(set string) string {
	return "; the table list of model group " + set + " may be incomplete"
}

// modelDiagnosticCount reads the count of a schema_model_unread or
// schema_model_core_tables message ("<file>: <n> ..."). A message about
// one declaration ("<file>:<line>: ...", the TypeScript linkers' form)
// counts 1. ok is false when the message has another shape.
func modelDiagnosticCount(d contract.Diagnostic) (int64, bool) {
	if rest, found := strings.CutPrefix(d.Message, d.File+":"); found {
		line, _, ok := strings.Cut(rest, ": ")
		if _, err := strconv.ParseInt(line, 10, 64); ok && err == nil && !strings.HasPrefix(line, " ") {
			return 1, true
		}
	}
	rest, found := strings.CutPrefix(d.Message, d.File+": ")
	if !found {
		return 0, false
	}
	digits, _, _ := strings.Cut(rest, " ")
	n, err := strconv.ParseInt(digits, 10, 64)
	return n, err == nil && n >= 0
}

// modelGroupState derives the model fields, table_list and
// diagnostic_counts of a model group. diagnostics are the stored
// diagnostics of the repository's Python files; those with a schema_model_
// code (and schema_unresolved_reference in a file of the group) belong to
// a group whose tool is sqlmodel, the Python linker's: SQLModel's group is
// the repository (root ".", T018g0, ADR 0058), so a repository has at most
// one and every Python file's model diagnostics are its own. A later
// front end's group takes the schema_ diagnostics of its own files.
//
// unfiled are the diagnostics without a stored file (the scanner's gaps).
// A .py file the index did not read (pythonNotRead) or a scanner gap that
// may hide one (pythonScanGap) may define tables of any model group, so
// the list is never complete with one (T018f Revision 2, B1): reasons
// python_unread and python_scan_gap.
func modelGroupState(s *schemaSet, tables []contract.Node, diagnostics, unfiled []contract.Diagnostic, read []string) {
	fields := &schemaModelFields{Files: []schemaModelFile{}}
	s.schemaModelFields = fields
	source := sourceModels
	s.Source = &source
	tool, conflict := commonString(tables, "tool")
	s.Tool = tool
	if conflict {
		s.Conflicts = append(s.Conflicts, "tool")
	}
	// The dialect a model group states (Prisma: the datasource's provider;
	// T018g1 review); null for a group that states none (SQLModel).
	dialect, conflict := commonString(tables, "dialect")
	s.Dialect = dialect
	if conflict {
		s.Conflicts = append(s.Conflicts, "dialect")
	}
	dialects, conflict := commonStringList(tables, "dialects")
	fields.Dialects = dialects
	if conflict {
		s.Conflicts = append(s.Conflicts, "dialects")
	}
	sort.Strings(s.Conflicts)
	// pins: the stored value when every table carries the same object.
	var first []byte
	for _, t := range tables {
		data, err := json.Marshal(t.Attributes["pins"])
		if _, isObject := t.Attributes["pins"].(map[string]any); !isObject || err != nil || first != nil && string(data) != string(first) {
			fields.Pins, fields.PinsConflict, first = nil, true, nil
			break
		}
		if first == nil {
			first, fields.Pins = data, t.Attributes["pins"]
		}
	}
	union := map[string]bool{}
	known := true
	for _, t := range tables {
		list, ok := stringArray(t.Attributes["assumptions"])
		if !ok {
			known = false
		}
		for _, a := range list {
			union[a] = true
		}
	}
	if known {
		fields.Assumptions = sortedKeys(union)
	}
	files := map[string]*schemaModelFile{}
	// The linker writes a count diagnostic only for a count above 0, so on
	// an index whose version proves it ran, a file without one has 0.
	modelsRead := slices.Contains(read, sourceModels)
	entry := func(f string) *schemaModelFile {
		if files[f] == nil {
			files[f] = &schemaModelFile{File: f}
			if modelsRead {
				var unread, core int64
				files[f].Unread, files[f].CoreTables = &unread, &core
			}
		}
		return files[f]
	}
	for _, t := range tables {
		entry(t.File).Tables++
	}
	sqlmodel := tool != nil && *tool == "sqlmodel"
	marker := ""
	if len(tables) > 0 && groupKey(tables[0]) != nil {
		marker = modelListMarker(*groupKey(tables[0]))
	}
	reasons := map[string]bool{}
	var unreadable []**int64
	for _, r := range pythonListReasons(diagnostics, unfiled) {
		reasons[r] = true
	}
	for _, d := range diagnostics {
		py := strings.HasSuffix(d.File, ".py")
		listed := !sqlmodel && marker != "" && strings.HasSuffix(d.Message, marker)
		mine := sqlmodel && py && strings.HasPrefix(d.Code, "schema_model_") || files[d.File] != nil && strings.HasPrefix(d.Code, "schema_") || listed && strings.HasPrefix(d.Code, "schema_")
		if !mine {
			continue
		}
		s.DiagnosticCounts[d.Code]++
		switch d.Code {
		case codeModelUnread, codeModelCoreTables:
			f := entry(d.File)
			dst, reason := &f.Unread, "model_unread"
			if d.Code == codeModelCoreTables {
				dst, reason = &f.CoreTables, "model_core_tables"
			}
			n, ok := modelDiagnosticCount(d)
			if !ok {
				reasons["model_counts_unknown"] = true
				unreadable = append(unreadable, dst)
				continue
			}
			if *dst != nil {
				n += **dst
			}
			*dst = &n
			if n > 0 {
				reasons[reason] = true
			}
		case codeModelImportFailure, codeModelNotClaimed:
			reasons["model_not_claimed"] = true
		}
	}
	for _, dst := range unreadable {
		*dst = nil
	}
	for _, f := range files {
		fields.Files = append(fields.Files, *f)
	}
	sort.Slice(fields.Files, func(i, j int) bool { return fields.Files[i].File < fields.Files[j].File })
	objects := objectsModeled
	s.Objects = &objects
	s.TableListReasons = sortedKeys(reasons)
	var list string
	switch {
	case len(reasons) > 0:
		list = tableListMaybe
	case modelsRead:
		list = tableListComplete
	}
	if list != "" {
		s.TableList = &list
	}
	s.TestOnly = testOnly(tables)
}

// stringList reads a JSON array of strings.
func stringArray(v any) ([]string, bool) {
	switch list := v.(type) {
	case []string:
		return list, true
	case []any:
		out := []string{}
		for _, x := range list {
			s, ok := x.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

func sortedKeys(m map[string]bool) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// tablesByScope counts tables by scope: test for a stored scope "test",
// production otherwise (O3: the view says how many Include tests hides).
func tablesByScope(tables []contract.Node) map[string]int {
	out := map[string]int{"production": 0, "test": 0}
	for _, t := range tables {
		if t.Attributes["scope"] == "test" {
			out["test"]++
		} else {
			out["production"]++
		}
	}
	return out
}

// schemaOverlap is the other table of a same-name model/migration pair
// (L5: a model table whose name equals a migration table's name). The
// column lists are present only when both column lists are proven: the
// migration table's state is complete and the model table's column_set is
// complete (L5's condition).
type schemaOverlap struct {
	Table            string   `json:"table"`
	Set              *string  `json:"set"`
	Source           string   `json:"source"`
	ColumnsOnlyHere  []string `json:"columns_only_here"`
	ColumnsOnlyThere []string `json:"columns_only_there"`
}

// columnsProven reports whether a table's column list is complete for L5.
func columnsProven(t contract.Node) bool {
	if modelRecord(t) {
		return t.Attributes["column_set"] == "complete"
	}
	return t.Attributes["state"] == "complete"
}

// overlapsProven reports whether table t's overlap list can be complete,
// so that it is null (unknown) otherwise, never []. A model table's other
// source is the migrations, and a migration tool the index found but did
// not read (unread_tools, 1.3) may create a table of the same name (T018f
// Revision 2, C6). A migration table's other source is the models, proven
// only when modelsComplete (modelListsComplete; F6 decision a).
func overlapsProven(t contract.Node, unread []schemaUnreadTool, modelsComplete bool) bool {
	if modelRecord(t) {
		return len(unread) == 0
	}
	return modelsComplete
}

// overlapsOf lists the pairs of table t among tables (by identity, other
// source only), with columnNames giving each table's stored column names.
func overlapsOf(t contract.Node, tables []contract.Node, columnNames func(string) []string) []schemaOverlap {
	out := []schemaOverlap{}
	name, ok := t.Attributes["name"].(string)
	if !ok {
		return out
	}
	for _, o := range tables {
		if modelRecord(o) == modelRecord(t) {
			continue
		}
		if other, _ := o.Attributes["name"].(string); other != name {
			continue
		}
		source := sourceMigrations
		if modelRecord(o) {
			source = sourceModels
		}
		entry := schemaOverlap{Table: o.Identity, Set: groupKey(o), Source: source}
		if columnsProven(t) && columnsProven(o) {
			here, there := map[string]bool{}, map[string]bool{}
			for _, c := range columnNames(t.Identity) {
				here[c] = true
			}
			for _, c := range columnNames(o.Identity) {
				there[c] = true
			}
			entry.ColumnsOnlyHere, entry.ColumnsOnlyThere = []string{}, []string{}
			for c := range here {
				if !there[c] {
					entry.ColumnsOnlyHere = append(entry.ColumnsOnlyHere, c)
				}
			}
			for c := range there {
				if !here[c] {
					entry.ColumnsOnlyThere = append(entry.ColumnsOnlyThere, c)
				}
			}
			sort.Strings(entry.ColumnsOnlyHere)
			sort.Strings(entry.ColumnsOnlyThere)
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Table < out[j].Table })
	return out
}

// schemaOpaqueSite is one opaque statement after a table's creation.
type schemaOpaqueSite struct {
	Migration string `json:"migration"`
	Span      any    `json:"span"`
	Row       any    `json:"row"`
	Kind      any    `json:"kind"`
}

// spanStart reads the start position of a stored span object.
func spanStart(v any) (line, column int64, ok bool) {
	m, isObject := v.(map[string]any)
	if !isObject {
		return 0, 0, false
	}
	l, okL := intValue(m["start_line"])
	c, okC := intValue(m["start_column"])
	return l, c, okL && okC
}

// tableOpaqueSites lists the stored opaque sites of the group's migrations
// that come after the table's creating statement: every site of a later
// migration and the sites of the creating migration that start after the
// table's span. Migrations with applied exactly false (down migrations,
// which sqlx never applies) are not read. The list is nil (unknown) when
// the table has no creating migration or span, a migration it needs has
// no opaque_sites list, a site has no readable span, or its length is not
// the table's integer opaque_after.
func tableOpaqueSites(t contract.Node, createdBy *string, migrations []contract.Node) []schemaOpaqueSite {
	if createdBy == nil || t.Span == nil {
		return nil
	}
	want, ok := intValue(t.Attributes["opaque_after"])
	if !ok {
		return nil
	}
	out := []schemaOpaqueSite{}
	after := false
	for _, m := range migrations {
		creator := m.Identity == *createdBy
		if !after && !creator {
			continue
		}
		after = true
		if m.Attributes["applied"] == false {
			continue
		}
		sites, isList := m.Attributes["opaque_sites"].([]any)
		if !isList {
			return nil
		}
		for _, raw := range sites {
			site, _ := raw.(map[string]any)
			line, column, ok := spanStart(site["span"])
			if !ok {
				return nil
			}
			if creator && (line < int64(t.Span.StartLine) || line == int64(t.Span.StartLine) && column <= int64(t.Span.StartColumn)) {
				continue
			}
			out = append(out, schemaOpaqueSite{Migration: m.Identity, Span: site["span"], Row: site["row"], Kind: site["kind"]})
		}
	}
	if !after || int64(len(out)) != want {
		return nil
	}
	return out
}

// claimRows reads the row ids of a record's claims; ok is false when the
// attribute is not a list of objects with a string row.
func claimRows(n contract.Node) (rows []string, ok bool) {
	list, isList := n.Attributes["claims"].([]any)
	if !isList {
		return nil, false
	}
	for _, raw := range list {
		c, _ := raw.(map[string]any)
		row, isString := c["row"].(string)
		if !isString {
			return nil, false
		}
		rows = append(rows, row)
	}
	return rows, true
}

// tableRestsOn is the assumptions every fact of a table rests on: for a
// migration table, the intersection of the assumption sets of the rows of
// every claim of its columns and indexes (nil when a record has no claims
// list, or no claim at all); for a model table, its stored assumptions.
func tableRestsOn(t contract.Node, records []contract.Node) []string {
	if modelRecord(t) {
		list, ok := stringArray(t.Attributes["assumptions"])
		if !ok {
			return nil
		}
		return sortedKeys(setOf(list...))
	}
	var common map[string]bool
	for _, r := range records {
		rows, ok := claimRows(r)
		if !ok {
			return nil
		}
		for _, row := range rows {
			set := setOf(migrationRowAssumptions()[row]...)
			if common == nil {
				common = set
				continue
			}
			for a := range common {
				if !set[a] {
					delete(common, a)
				}
			}
		}
	}
	if common == nil {
		return nil
	}
	return sortedKeys(common)
}

// groupRestsOn is the union of the assumption sets of every row behind a
// claim, reason or opaque site of a migration group, or of the stored
// assumptions of a model group's tables. It is nil when the group's records
// predate the provenance attributes (a column or index without a claims
// list, a reason without a row key, a migration without opaque_sites) or
// a model table has no assumptions list.
func groupRestsOn(model bool, tables, records, migrations []contract.Node) []string {
	union := map[string]bool{}
	add := func(row string) {
		for _, a := range migrationRowAssumptions()[row] {
			union[a] = true
		}
	}
	if model {
		for _, t := range tables {
			list, ok := stringArray(t.Attributes["assumptions"])
			if !ok {
				return nil
			}
			for _, a := range list {
				union[a] = true
			}
		}
		return sortedKeys(union)
	}
	for _, r := range records {
		rows, ok := claimRows(r)
		if !ok {
			return nil
		}
		for _, row := range rows {
			add(row)
		}
	}
	for _, t := range tables {
		for _, raw := range objectsOrRaw(t.Attributes["reasons"]) {
			m, isObject := raw.(map[string]any)
			if !isObject {
				continue
			}
			row, present := m["row"]
			if !present {
				return nil
			}
			if id, isString := row.(string); isString {
				add(id)
			}
		}
	}
	for _, m := range migrations {
		if _, present := m.Attributes["opaque_sites"]; !present {
			return nil
		}
		for _, raw := range objectsOrRaw(m.Attributes["opaque_sites"]) {
			if site, isObject := raw.(map[string]any); isObject {
				if row, isString := site["row"].(string); isString {
					add(row)
				}
			}
		}
	}
	return sortedKeys(union)
}

// verificationCounts is a model table's stored verification_counts (its
// columns' claims counted by verification when the table was written, ADR
// 0057); nil when the record has no verification (an index built before
// extractors-31: T018g0 Revision 2) or no well-formed counts (written
// before ADR 0057), so a table is never served as "no unverified fact"
// without a record that says so.
func verificationCounts(table contract.Node) map[string]int {
	if _, has := table.Attributes["verification"]; !has {
		return nil
	}
	stored, isObject := table.Attributes["verification_counts"].(map[string]any)
	if !isObject {
		return nil
	}
	counts := make(map[string]int, len(stored))
	for v, raw := range stored {
		n, ok := intValue(raw)
		if !ok || n < 0 {
			return nil
		}
		counts[v] = int(n)
	}
	return counts
}

// servedTable is a table node as /schema/sets and /schema/table serve it:
// without the stored verification_counts, which /schema/sets serves next to
// the node (schemaSetTable.VerificationCounts), so both keep the shape they
// had before the counts were stored (ADR 0057).
func servedTable(t contract.Node) contract.Node {
	if _, has := t.Attributes["verification_counts"]; !has {
		return t
	}
	attrs := make(map[string]any, len(t.Attributes)-1)
	for k, v := range t.Attributes {
		if k != "verification_counts" {
			attrs[k] = v
		}
	}
	t.Attributes = attrs
	return t
}

// setColumnNode is a column as /schema/sets serves it: a model column
// without its claims, which /schema/table serves with their links (T018g0
// Revision 1: serving every model column's claims made langflow's
// /schema/sets 54 % slower); a migration column verbatim. The store already
// reads /schema/sets' model columns without claims (ADR 0057);
// this keeps the served shape for any column that still has them.
func setColumnNode(col contract.Node) contract.Node {
	if !modelRecord(col) {
		return col
	}
	if _, has := col.Attributes["claims"]; !has {
		return col
	}
	attrs := make(map[string]any, len(col.Attributes)-1)
	for k, v := range col.Attributes {
		if k != "claims" {
			attrs[k] = v
		}
	}
	col.Attributes = attrs
	return col
}

// commonStringList is the string list every node stores under key (ADR
// 0065: a model table's run-time dialects); nil when no node stores one,
// and nil with conflict when the nodes differ, one lacks it or a value is
// not a list of strings.
func commonStringList(nodes []contract.Node, key string) (value []string, conflict bool) {
	var first []string
	present, missing := false, false
	for _, n := range nodes {
		list, ok := stringArray(n.Attributes[key])
		if !ok {
			missing = true
			continue
		}
		if present && !slices.Equal(list, first) {
			return nil, true
		}
		first, present = list, true
	}
	switch {
	case !present:
		return nil, false
	case missing:
		return nil, true
	}
	return first, false
}

// tableDialects is a model table's stored run-time dialects (ADR 0065);
// nil for any other table or a value that is not a list of strings.
func tableDialects(table contract.Node) []string {
	if !modelRecord(table) {
		return nil
	}
	list, ok := stringArray(table.Attributes["dialects"])
	if !ok {
		return nil
	}
	return list
}
