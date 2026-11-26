//go:build ignore

// The GORM oracle of T018g5 (ADR 0063). Never run by CI, never
// built in the repository: the build constraint keeps it out of the
// graphvein module. scripts/oracle/gorm_catalog.sh copies this directory
// into a scratch Go module (without the constraint line), copies the
// model packages there with scripts/oracle/gorm_oracle/copytypes.go (types,
// constants, literal variables and the methods GORM's schema parse calls;
// every other function, and so every side effect, dropped) and builds one
// binary per run file.
//
// A run is one module's migrate sequence as its source states it (the
// gorm.Config the module's gorm.Open site passes, the calls in source
// order). The binary runs it on one dialect: GORM's own AutoMigrate or
// Migrator().CreateTable creates the tables in a fresh database (postgres:
// the scratch Postgres 17 of scripts/cloud-postgres.sh; sqlite: a scratch
// file through the pure-Go driver github.com/glebarez/sqlite),
// every call runs even when an earlier one fails (each error is
// recorded), and the catalog is dumped as JSON with GORM's parsed schema of
// every listed model.
//
// Usage (in the scratch module):
//
//	oracle -list                      one line per run: name|dialect
//	oracle -run NAME -dialect D -dsn DSN -out FILE
//
// On postgres DSN is a URL of a role that may create databases: the run
// creates a fresh database gorm_oracle_<run>, migrates into it and drops it
// after the dump. On sqlite DSN is a scratch file, removed first.
//
//	oracle -merge OUT IN...           writes {"runs": [...]}
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"runtime/debug"
	"sort"
	"strings"

	glebarez "github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// step is one migrate call of a run.
type step struct {
	// Site is the call's file:line in the module (documentation only).
	Site string
	// CreateTable runs Migrator().CreateTable instead of AutoMigrate
	// (Migrator().AutoMigrate is AutoMigrate).
	CreateTable bool
	Models      []any
	// Do, when set, is another call of the module's source that changes
	// the schema (SetupJoinTable, a Migrator DDL call, Exec, a migrate call
	// on a scoped handle; T018g5 Revision 2), run in place of the migrate
	// call; Call names it in the dump.
	Do   func(db *gorm.DB) error
	Call string
}

// run is one module's migrate sequence.
type run struct {
	Name string
	// Repo is the checkout the module is in, relative to the comparison
	// root ("." for the fixture), and Group the model group's set
	// (models/gorm/<module dir>).
	Repo, Group string
	// Dialects the run is made on; CrossDialect marks a dialect the
	// module does not open (only dialect-independent facts are compared).
	Dialects     []string
	CrossDialect map[string]bool
	// Config builds the gorm.Config of the module's gorm.Open site.
	Config func() gorm.Config
	Steps  []step
	// Setup are SQL statements run before the steps, per dialect (the
	// database objects the application assumes, GO-V2).
	Setup map[string][]string
}

var runs []run

func register(r run) { runs = append(runs, r) }

func main() {
	list := flag.Bool("list", false, "print the runs")
	name := flag.String("run", "", "run name")
	dialect := flag.String("dialect", "", "postgres or sqlite")
	dsn := flag.String("dsn", "", "database DSN (postgres URL or sqlite file)")
	out := flag.String("out", "", "output file")
	merge := flag.Bool("merge", false, "merge run dumps: -merge OUT IN...")
	flag.Parse()
	switch {
	case *list:
		for _, r := range runs {
			for _, d := range r.Dialects {
				fmt.Printf("%s|%s\n", r.Name, d)
			}
		}
	case *merge:
		if err := mergeDumps(flag.Args()); err != nil {
			fmt.Fprintln(os.Stderr, "merge:", err)
			os.Exit(1)
		}
	default:
		if err := execute(*name, *dialect, *dsn, *out); err != nil {
			fmt.Fprintln(os.Stderr, *name+":", err)
			os.Exit(1)
		}
	}
}

// dump is one run on one dialect as the comparison reads it.
type dump struct {
	Name         string            `json:"name"`
	Repo         string            `json:"repo"`
	Group        string            `json:"group"`
	Dialect      string            `json:"dialect"`
	Driver       string            `json:"driver"`
	CrossDialect bool              `json:"cross_dialect"`
	Versions     map[string]string `json:"versions"`
	Setup        []string          `json:"setup"`
	Steps        []stepDump        `json:"steps"`
	Schemas      []schemaDump      `json:"schemas"`
	Catalog      *catalog          `json:"catalog"`
}

type stepDump struct {
	Site        string   `json:"site"`
	Call        string   `json:"call,omitempty"`
	CreateTable bool     `json:"create_table"`
	Models      []string `json:"models"`
	Error       string   `json:"error"`
}

func execute(name, dialect, dsn, out string) error {
	var r *run
	for i := range runs {
		if runs[i].Name == name {
			r = &runs[i]
		}
	}
	if r == nil {
		return fmt.Errorf("unknown run")
	}
	cfg := gorm.Config{}
	if r.Config != nil {
		cfg = r.Config()
	}
	// The logger only prints; it changes no DDL.
	cfg.Logger = logger.Default.LogMode(logger.Silent)
	var d gorm.Dialector
	driver := dialect
	switch dialect {
	case "postgres":
		name := "gorm_oracle_" + strings.NewReplacer("-", "_", ".", "_").Replace(r.Name)
		admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			return fmt.Errorf("opening %s: %w", dsn, err)
		}
		for _, q := range []string{"DROP DATABASE IF EXISTS " + name, "CREATE DATABASE " + name} {
			if err := admin.Exec(q).Error; err != nil {
				return fmt.Errorf("%s: %w", q, err)
			}
		}
		defer func() {
			if sqldb, err := admin.DB(); err == nil {
				sqldb.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
				sqldb.Close()
			}
		}()
		u, err := url.Parse(dsn)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", dsn, err)
		}
		u.Path = "/" + name
		d = postgres.Open(u.String())
	case "sqlite":
		if err := os.Remove(dsn); err != nil && !os.IsNotExist(err) {
			return err
		}
		d = glebarez.Open(dsn)
		driver = "glebarez"
	default:
		return fmt.Errorf("dialect %q", dialect)
	}
	db, err := gorm.Open(d, &cfg)
	if err != nil {
		return fmt.Errorf("opening %s: %w", dialect, err)
	}
	res := dump{Name: r.Name, Repo: r.Repo, Group: r.Group, Dialect: dialect, Driver: driver, CrossDialect: r.CrossDialect[dialect], Versions: versions(), Setup: r.Setup[dialect]}
	for _, q := range r.Setup[dialect] {
		if err := db.Exec(q).Error; err != nil {
			return fmt.Errorf("setup %q: %w", q, err)
		}
	}
	for _, s := range r.Steps {
		sd := stepDump{Site: s.Site, CreateTable: s.CreateTable, Call: s.Call}
		for _, m := range s.Models {
			sd.Models = append(sd.Models, fmt.Sprintf("%T", m))
		}
		if s.Do != nil {
			err = s.Do(db)
		} else if s.CreateTable {
			err = db.Migrator().CreateTable(s.Models...)
		} else {
			err = db.AutoMigrate(s.Models...)
		}
		if err != nil {
			sd.Error = err.Error()
		}
		res.Steps = append(res.Steps, sd)
	}
	for _, s := range r.Steps {
		for _, m := range s.Models {
			res.Schemas = append(res.Schemas, dumpSchema(db, m))
		}
	}
	sqldb, err := db.DB()
	if err != nil {
		return err
	}
	if dialect == "postgres" {
		res.Catalog, err = pgCatalog(sqldb)
	} else {
		res.Catalog, err = sqliteCatalog(sqldb)
	}
	if err != nil {
		return fmt.Errorf("reading the catalog: %w", err)
	}
	data, err := json.MarshalIndent(res, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, append(data, '\n'), 0o644)
}

// versions are the module versions the binary was built with (GO-V1: the
// comparison checks gorm's against the record's pins).
func versions() map[string]string {
	out := map[string]string{}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return out
	}
	for _, m := range info.Deps {
		switch m.Path {
		case "gorm.io/gorm", "gorm.io/driver/postgres", "github.com/glebarez/sqlite", "github.com/jinzhu/inflection", "github.com/jackc/pgx/v5", "modernc.org/sqlite":
			out[m.Path] = m.Version
		}
	}
	return out
}

// schemaDump is GORM's parsed schema of one model after the run (for
// reading a disagreement; the comparison uses the catalog).
type schemaDump struct {
	Model      string      `json:"model"`
	Table      string      `json:"table"`
	ParseError string      `json:"parse_error,omitempty"`
	Fields     []fieldDump `json:"fields"`
	Relations  []string    `json:"relations"`
}

type fieldDump struct {
	Name          string `json:"name"`
	DBName        string `json:"db_name"`
	DataType      string `json:"data_type"`
	Size          int    `json:"size"`
	Precision     int    `json:"precision"`
	Scale         int    `json:"scale"`
	PrimaryKey    bool   `json:"primary_key"`
	AutoIncrement bool   `json:"autoincrement"`
	NotNull       bool   `json:"not_null"`
	Unique        bool   `json:"unique"`
	Default       string `json:"default,omitempty"`
}

func dumpSchema(db *gorm.DB, m any) schemaDump {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(m); err != nil {
		return schemaDump{Model: fmt.Sprintf("%T", m), ParseError: err.Error()}
	}
	s := stmt.Schema
	out := schemaDump{Model: s.Name, Table: s.Table}
	for _, f := range s.Fields {
		if f.DBName == "" {
			continue
		}
		out.Fields = append(out.Fields, fieldDump{f.Name, f.DBName, string(f.DataType), f.Size, f.Precision, f.Scale, f.PrimaryKey, f.AutoIncrement, f.NotNull, f.Unique, f.DefaultValue})
	}
	for n, rel := range s.Relationships.Relations {
		out.Relations = append(out.Relations, fmt.Sprintf("%s %s -> %s", n, rel.Type, rel.FieldSchema.Table))
	}
	sort.Strings(out.Relations)
	return out
}

func mergeDumps(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("want OUT IN...")
	}
	var all []json.RawMessage
	for _, p := range args[1:] {
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		all = append(all, json.RawMessage(strings.TrimSpace(string(data))))
	}
	data, err := json.MarshalIndent(map[string]any{"runs": all}, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(args[0], append(data, '\n'), 0o644)
}
