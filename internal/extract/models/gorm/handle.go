package gorm

import (
	"fmt"
	"strconv"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Dialects GORM discovery supports (row GO-K2).
const (
	DialectPostgres = "postgres"
	DialectSQLite   = "sqlite"
	DialectMySQL    = "mysql"
)

// dialectors maps a dialector package to its dialect and HandleForms form.
var dialectors = map[string][2]string{
	pkgDrivers + "postgres": {DialectPostgres, "postgres"},
	pkgDrivers + "sqlite":   {DialectSQLite, "sqlite"},
	pkgGlebarez:             {DialectSQLite, "glebarez sqlite"},
	pkgDrivers + "mysql":    {DialectMySQL, "mysql"},
}

// OpenSite is one gorm.Open(<dialector>, <config>) call of a read file.
type OpenSite struct {
	File, Func string
	Line       int
	Nesting    string
	// Driver is the dialector's import path and Constructor "Open" or "New"
	// (both empty when the dialector is not a call of an imported package);
	// Dialect is "" when the dialector is not one GORM discovery supports,
	// and Reason then says why.
	Driver, Constructor, Dialect, Reason string
	Config                               Config
	// MySQL holds the DDL settings of a mysql dialector.
	MySQL MySQLConfig
}

// formNoConfig is the GO-K2 form of a gorm.Open call without a
// configuration argument.
const formNoConfig = "no configuration argument"

// Config is the part of a gorm.Config that decides table facts (row
// GO-K2). Form is "config literal", "once-defined local" or
// formNoConfig (GORM's zero Config); "" when the configuration is not
// read, and every setting is then unknown.
type Config struct {
	Form string
	// Line is the literal's line; Keys its keys in order as "Key=value",
	// the value "true", "false" or "nonliteral" (reports and tests).
	Line int
	Keys []string
	// Naming is the naming strategy: Known false when not read (every
	// derived name unknown), else the literal fields (zero value: the
	// default strategy).
	Naming NamingSetting
	// DisableForeignKeys and IgnoreRelationships are the two boolean keys.
	DisableForeignKeys, IgnoreRelationships Flag
}

// Flag is a boolean setting: Known false when it is not proven.
type Flag struct {
	Known, Value bool
	Reason       string
}

// NamingSetting is a NamingStrategy setting: Known false when not proven.
type NamingSetting struct {
	Known  bool
	Naming Naming
	Reason string
}

// Naming is a literal schema.NamingStrategy (the zero value is GORM's
// default strategy). IdentifierMaxLength 0 is the dialector's default.
type Naming struct {
	TablePrefix                string
	SingularTable, NoLowerCase bool
	IdentifierMaxLength        int
}

// MySQLConfig holds the mysql dialector settings that change DDL (the
// dialect rule: DefaultStringSize, DisableDatetimePrecision,
// DefaultDatetimePrecision), by key.
type MySQLConfig map[string]Setting

// Setting is one dialector setting: Known false when not proven; Set false
// for the driver's default; Value the literal's source text.
type Setting struct {
	Known, Set bool
	Value      string
	Reason     string
}

// mysqlKeys are the mysql.Config keys that change DDL.
var mysqlKeys = []string{"DefaultStringSize", "DisableDatetimePrecision", "DefaultDatetimePrecision"}

// openSite reads a gorm.Open call (row GO-K2).
func (r *resolver) openSite(fn *facts.GoFunc, s facts.GoSite) {
	o := &OpenSite{File: r.path, Func: funcName(fn), Line: s.Line, Nesting: s.Nesting}
	r.m.Opens = append(r.m.Opens, o)
	r.d.in.tag("GO-K2")
	args := s.Call.Args
	if len(args) == 0 {
		o.Reason = "gorm.Open has no dialector"
		return
	}
	var extra []*OpenSite
	if a := args[0]; a.Kind == facts.GoIdentifier && declaresLocal(fn, a.Name) {
		extra = r.dialectorLocal(o, fn, a.Name)
		defer func() {
			// The other dialectors of the local open with the same
			// configuration.
			for _, x := range extra {
				x.Config = o.Config
				r.m.Opens = append(r.m.Opens, x)
			}
		}()
	} else {
		r.dialector(o, args[0])
	}
	switch {
	case len(args) == 1:
		// gorm.Open(d) runs with GORM's zero Config (gorm.go:138-179):
		// the default naming strategy, foreign keys on, relations read
		// (Revision 1 addendum 2, P1).
		r.d.in.tag("GO-K2/" + formNoConfig)
		o.Config = Config{Form: formNoConfig, Line: o.Line, Naming: NamingSetting{Known: true}, DisableForeignKeys: Flag{Known: true}, IgnoreRelationships: Flag{Known: true}}
	case len(args) > 2:
		o.Config.unknown("gorm.Open has several options")
	default:
		r.config(o, fn, args[1])
	}
}

// formDialectorLocal is the GO-K2 form of a dialector held in a local.
const formDialectorLocal = "a `gorm.Dialector` local assigned dialectors"

// dialectorLocal reads a gorm.Open whose dialector is a local of the same
// function (T018g5 Revision 2, coverage): `var d gorm.Dialector` (or a
// define) whose every value, wherever it is assigned in the function, is
// a dialector call the contract reads. The site then opens each assigned
// dialector at run time: one OpenSite per value, so the group's dialects
// are their union (GO-V3); any other value leaves the dialect unknown.
func (r *resolver) dialectorLocal(o *OpenSite, fn *facts.GoFunc, name string) []*OpenSite {
	var values []facts.GoExpr
	for _, l := range fn.Locals {
		for i, n := range l.Names {
			if n != name {
				continue
			}
			switch {
			case len(l.Values) == len(l.Names):
				values = append(values, l.Values[i])
			case len(l.Values) == 0 && l.Kind == "var" && l.Type != nil && l.Type.Kind == facts.GoSelector && l.Type.Name == "Dialector" && r.importOf(r.path, l.Type.Object) == pkgGorm:
				// var d gorm.Dialector: nil until assigned.
			default:
				o.Reason = "the dialector " + name + " is not a local assigned dialector calls only"
				return nil
			}
		}
	}
	if len(values) == 0 {
		o.Reason = "the dialector " + name + " is never assigned"
		return nil
	}
	r.d.in.tag("GO-K2/" + formDialectorLocal)
	first := *o
	var extra []*OpenSite
	for i, v := range values {
		site := o
		if i > 0 {
			c := first
			site = &c
		}
		r.dialector(site, v)
		if site.Dialect == "" {
			o.Reason, o.Dialect = site.Reason, ""
			return nil
		}
		if i > 0 {
			extra = append(extra, site)
		}
	}
	return extra
}

// declaresLocal reports a var or define local of fn named name that is
// not also a parameter or the receiver.
func declaresLocal(fn *facts.GoFunc, name string) bool {
	if fn.Receiver != nil && fn.Receiver.Name == name {
		return false
	}
	for _, p := range fn.Params {
		if p.Name == name {
			return false
		}
	}
	return declares(fn, name)
}

// dialector reads the first argument of gorm.Open.
func (r *resolver) dialector(o *OpenSite, e facts.GoExpr) {
	if e.Kind != facts.GoCall || e.Callee.Kind != facts.GoSelector || e.Callee.Object.Kind != facts.GoIdentifier {
		o.Reason = "the dialector is not a call of a driver package"
		return
	}
	o.Driver, o.Constructor = r.importOf(r.path, e.Callee.Object), e.Callee.Name
	d, ok := dialectors[o.Driver]
	if !ok || o.Constructor != "Open" && o.Constructor != "New" {
		o.Reason = fmt.Sprintf("dialector %s.%s is not one GORM discovery reads", o.Driver, o.Constructor)
		if o.Driver == "" {
			o.Reason = "the dialector is not a call of an imported driver package"
		}
		return
	}
	o.Dialect = d[0]
	r.d.in.tag("GO-K2/" + d[1])
	if o.Dialect != DialectMySQL {
		return
	}
	o.MySQL = MySQLConfig{}
	for _, k := range mysqlKeys {
		o.MySQL[k] = Setting{Known: true}
	}
	if o.Constructor == "Open" {
		return
	}
	var cfg *facts.GoExpr
	if len(e.Args) == 1 && e.Args[0].Kind == facts.GoComposite && e.Args[0].Type != nil && e.Args[0].Type.Kind == facts.GoSelector && e.Args[0].Type.Name == "Config" && r.importOf(r.path, e.Args[0].Type.Object) == o.Driver {
		cfg = &e.Args[0]
	}
	if cfg == nil {
		for _, k := range mysqlKeys {
			o.MySQL[k] = Setting{Reason: "mysql.New's argument is not a mysql.Config literal"}
		}
		return
	}
	r.d.in.tag("GO-K2/`mysql.New(mysql.Config{...})`")
	for _, el := range cfg.Elements {
		if el.Key == nil || el.Key.Kind != facts.GoIdentifier {
			for _, k := range mysqlKeys {
				o.MySQL[k] = Setting{Reason: "the mysql.Config literal has positional elements"}
			}
			return
		}
		if _, read := o.MySQL[el.Key.Name]; !read {
			continue
		}
		switch el.Value.Kind {
		case facts.GoNumber, facts.GoBoolean:
			o.MySQL[el.Key.Name] = Setting{Known: true, Set: true, Value: el.Value.Value}
		default:
			o.MySQL[el.Key.Name] = Setting{Reason: "mysql.Config " + el.Key.Name + " is not a literal"}
		}
	}
}

func (c *Config) unknown(why string) {
	c.Form = ""
	c.Naming = NamingSetting{Reason: why}
	c.DisableForeignKeys = Flag{Reason: why}
	c.IgnoreRelationships = Flag{Reason: why}
}

// config reads gorm.Open's configuration argument: a &gorm.Config{...}
// literal, or a local of the same function defined once from one and
// never assigned again.
func (r *resolver) config(o *OpenSite, fn *facts.GoFunc, e facts.GoExpr) {
	form, line := "config literal", o.Line
	if e.Kind == facts.GoIdentifier {
		form = "once-defined local"
		v, at, why := onceDefined(fn, e.Name)
		if v == nil {
			o.Config.unknown("the configuration " + e.Name + " " + why)
			return
		}
		e, line = *v, at
	}
	lit := e.Object
	if e.Kind != facts.GoUnary || e.Value != "&" || lit == nil || lit.Kind != facts.GoComposite || lit.Type == nil || lit.Type.Kind != facts.GoSelector || lit.Type.Name != "Config" || r.importOf(r.path, lit.Type.Object) != pkgGorm {
		o.Config.unknown("the configuration is not a &gorm.Config{...} literal")
		return
	}
	r.d.in.tag("GO-K2/" + form)
	c := Config{Form: form, Line: line, Naming: NamingSetting{Known: true}, DisableForeignKeys: Flag{Known: true}, IgnoreRelationships: Flag{Known: true}}
	for _, el := range lit.Elements {
		if el.Key == nil || el.Key.Kind != facts.GoIdentifier {
			o.Config.unknown("the gorm.Config literal has positional elements")
			return
		}
		v := "nonliteral"
		if el.Value.Kind == facts.GoBoolean {
			v = el.Value.Value
		}
		c.Keys = append(c.Keys, el.Key.Name+"="+v)
		switch el.Key.Name {
		case "NamingStrategy":
			r.d.in.tag("GO-K2/`NamingStrategy` (literal)")
			c.Naming = r.naming(el.Value)
		case "DisableForeignKeyConstraintWhenMigrating":
			r.d.in.tag("GO-K2/`DisableForeignKeyConstraintWhenMigrating`")
			c.DisableForeignKeys = flag(el.Value, el.Key.Name)
		case "IgnoreRelationshipsWhenMigrating":
			r.d.in.tag("GO-K2/`IgnoreRelationshipsWhenMigrating`")
			c.IgnoreRelationships = flag(el.Value, el.Key.Name)
		case "DryRun":
			// DryRun builds the migration's SQL without running it, so no
			// row covers a configuration that may set it (row D).
			if el.Value.Kind != facts.GoBoolean || el.Value.Value != "false" {
				o.Dialect, o.Reason = "", "the configuration sets DryRun, so the migration may not run"
			}
		}
	}
	o.Config = c
}

// onceDefined returns the value a local is defined from when it has exactly
// one declaration with a value in fn and no assignment.
func onceDefined(fn *facts.GoFunc, name string) (*facts.GoExpr, int, string) {
	var value *facts.GoExpr
	n, line := 0, 0
	for i := range fn.Locals {
		l := &fn.Locals[i]
		for j, x := range l.Names {
			if x != name {
				continue
			}
			if l.Kind == "assign" {
				return nil, 0, "is assigned again"
			}
			n++
			if len(l.Values) == len(l.Names) {
				value, line = &l.Values[j], l.Line
			}
		}
	}
	switch {
	case n == 0:
		return nil, 0, "is not a local of the function"
	case n > 1:
		return nil, 0, "is declared more than once"
	case value == nil:
		return nil, 0, "is not defined from one value"
	}
	return value, line, ""
}

func flag(e facts.GoExpr, key string) Flag {
	if e.Kind == facts.GoBoolean {
		return Flag{Known: true, Value: e.Value == "true"}
	}
	return Flag{Reason: key + " is not a literal boolean"}
}

// naming reads a NamingStrategy value: a schema.NamingStrategy{...} literal
// (or & of one) whose read fields are literals.
func (r *resolver) naming(e facts.GoExpr) NamingSetting {
	if e.Kind == facts.GoUnary && e.Value == "&" && e.Object != nil {
		e = *e.Object
	}
	if e.Kind != facts.GoComposite || e.Type == nil || e.Type.Kind != facts.GoSelector || e.Type.Name != "NamingStrategy" || r.importOf(r.path, e.Type.Object) != pkgSchema {
		return NamingSetting{Reason: "NamingStrategy is not a schema.NamingStrategy{...} literal"}
	}
	var n Naming
	for _, el := range e.Elements {
		if el.Key == nil || el.Key.Kind != facts.GoIdentifier {
			return NamingSetting{Reason: "the NamingStrategy literal has positional elements"}
		}
		v := el.Value
		bad := NamingSetting{Reason: "NamingStrategy " + el.Key.Name + " is not a literal"}
		switch el.Key.Name {
		case "TablePrefix":
			if v.Kind != facts.GoString {
				return bad
			}
			n.TablePrefix = v.Value
		case "SingularTable", "NoLowerCase":
			if v.Kind != facts.GoBoolean {
				return bad
			}
			if el.Key.Name == "SingularTable" {
				n.SingularTable = v.Value == "true"
			} else {
				n.NoLowerCase = v.Value == "true"
			}
		case "IdentifierMaxLength":
			x, err := strconv.Atoi(v.Value)
			if v.Kind != facts.GoNumber || err != nil {
				return bad
			}
			n.IdentifierMaxLength = x
		default:
			// NameReplacer and any other field: every derived name unknown.
			return NamingSetting{Reason: "NamingStrategy sets " + el.Key.Name}
		}
	}
	return NamingSetting{Known: true, Naming: n}
}

// mutationKeys maps an assigned selector name (GoMutation) to the settings
// it makes unknown (GO-K2, GO-V4).
var mutationKeys = map[string]string{
	"Config": "all", "NamingStrategy": "naming", "TablePrefix": "naming", "SingularTable": "naming",
	"NoLowerCase": "naming", "IdentifierMaxLength": "naming", "NameReplacer": "naming",
	"DisableForeignKeyConstraintWhenMigrating": "fk", "IgnoreRelationshipsWhenMigrating": "relations",
}

// mergeConfig merges the Open sites' configurations into the module's: a
// key is known only when every site reads it and they agree, and no
// mutation in a read file changes it. It also sets the module's dialects
// and refusal.
func (r *resolver) mergeConfig() {
	m := r.m
	seen := map[string]bool{}
	for _, o := range m.Opens {
		if o.Dialect == "" {
			if m.Refused == "" {
				m.Refused = fmt.Sprintf("%s:%d: %s", o.File, o.Line, o.Reason)
			}
			continue
		}
		if !seen[o.Dialect] {
			seen[o.Dialect] = true
			m.Dialects = append(m.Dialects, o.Dialect)
		}
	}
	sortStrings(m.Dialects)
	if len(m.Dialects) == 1 {
		m.Dialect = m.Dialects[0]
	}
	if len(m.Opens) == 0 {
		return
	}
	c := m.Opens[0].Config
	for _, o := range m.Opens[1:] {
		if c.Form != o.Config.Form {
			c.Form = "mixed"
		}
		if o.Config.Naming != c.Naming {
			if c.Naming.Known && o.Config.Naming.Known {
				c.Naming = NamingSetting{Reason: "the gorm.Open sites disagree on NamingStrategy"}
			} else if c.Naming.Known {
				c.Naming = o.Config.Naming
			}
		}
		c.DisableForeignKeys = agree(c.DisableForeignKeys, o.Config.DisableForeignKeys, "DisableForeignKeyConstraintWhenMigrating")
		c.IgnoreRelationships = agree(c.IgnoreRelationships, o.Config.IgnoreRelationships, "IgnoreRelationshipsWhenMigrating")
	}
	for _, mu := range r.mutations {
		why := fmt.Sprintf("%s is assigned at %s", mu.name, mu.at)
		switch mutationKeys[mu.name] {
		case "all":
			c.Naming, c.DisableForeignKeys, c.IgnoreRelationships = NamingSetting{Reason: why}, Flag{Reason: why}, Flag{Reason: why}
		case "naming":
			c.Naming = NamingSetting{Reason: why}
		case "fk":
			c.DisableForeignKeys = Flag{Reason: why}
		case "relations":
			c.IgnoreRelationships = Flag{Reason: why}
		}
	}
	m.Config = c
	m.MySQL = nil
	for _, o := range m.Opens {
		if o.Dialect != DialectMySQL {
			continue
		}
		if m.MySQL == nil {
			m.MySQL = MySQLConfig{}
			for k, v := range o.MySQL {
				m.MySQL[k] = v
			}
			continue
		}
		for _, k := range mysqlKeys {
			a, b := m.MySQL[k], o.MySQL[k]
			if a != b && a.Known && b.Known {
				m.MySQL[k] = Setting{Reason: "the mysql sites disagree on " + k}
			} else if a.Known && !b.Known {
				m.MySQL[k] = b
			}
		}
	}
}

// agree merges one boolean key of two sites.
func agree(a, b Flag, key string) Flag {
	switch {
	case !a.Known:
		return a
	case !b.Known:
		return b
	case a.Value != b.Value:
		return Flag{Reason: "the gorm.Open sites disagree on " + key}
	}
	return a
}
