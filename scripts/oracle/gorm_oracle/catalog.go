//go:build ignore

package main

import (
	"database/sql"
	"regexp"
	"strings"
)

// catalog is the scratch database after a run, reduced to the facts the
// comparison reads (task file, "Oracle design": postgres pg_class,
// pg_attribute, pg_constraint, pg_index; sqlite sqlite_master and the
// pragmas).
type catalog struct {
	Tables []table `json:"tables"`
}

type table struct {
	Name    string   `json:"name"`
	Columns []column `json:"columns"`
	// PrimaryKey in key order.
	PrimaryKey []string `json:"primary_key"`
	// Uniques are unique constraints (postgres contype u; sqlite indexes
	// of origin u, whose names SQLite chooses and are not kept).
	Uniques []unique `json:"uniques"`
	// Indexes are the indexes that back no constraint (postgres) or that
	// CREATE INDEX made (sqlite origin c).
	Indexes     []index      `json:"indexes"`
	ForeignKeys []foreignKey `json:"foreign_keys"`
	Checks      int          `json:"checks"`
	// SQL is SQLite's CREATE TABLE text.
	SQL string `json:"sql,omitempty"`
}

type column struct {
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	NotNull bool    `json:"not_null"`
	Default *string `json:"default"`
	// Autoincrement: postgres a nextval default or an identity; sqlite an
	// inline INTEGER PRIMARY KEY AUTOINCREMENT.
	Autoincrement bool `json:"autoincrement"`
	Generated     bool `json:"generated"`
}

type unique struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
}

type index struct {
	Name string `json:"name"`
	// Columns in key order; an expression key is "".
	Columns    []string `json:"columns"`
	Descending []bool   `json:"descending"`
	Unique     bool     `json:"unique"`
	Partial    bool     `json:"partial"`
	Method     string   `json:"method,omitempty"`
}

type foreignKey struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns"`
	RefTable   string   `json:"ref_table"`
	RefColumns []string `json:"ref_columns"`
	OnDelete   string   `json:"on_delete"`
	OnUpdate   string   `json:"on_update"`
}

var pgActions = map[string]string{"a": "NO ACTION", "r": "RESTRICT", "c": "CASCADE", "n": "SET NULL", "d": "SET DEFAULT"}

func split(s sql.NullString) []string {
	if !s.Valid || s.String == "" {
		return []string{}
	}
	return strings.Split(s.String, "\x1f")
}

func pgCatalog(db *sql.DB) (*catalog, error) {
	out := &catalog{Tables: []table{}}
	trows, err := db.Query(`select c.oid, c.relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public' and c.relkind in ('r', 'p') order by c.relname`)
	if err != nil {
		return nil, err
	}
	type rel struct {
		oid  int64
		name string
	}
	var rels []rel
	for trows.Next() {
		var r rel
		if err := trows.Scan(&r.oid, &r.name); err != nil {
			return nil, err
		}
		rels = append(rels, r)
	}
	trows.Close()
	for _, r := range rels {
		t := table{Name: r.name, Columns: []column{}, PrimaryKey: []string{}, Uniques: []unique{}, Indexes: []index{}, ForeignKeys: []foreignKey{}}
		crows, err := db.Query(`select a.attname, format_type(a.atttypid, a.atttypmod), a.attnotnull,
			pg_get_expr(d.adbin, d.adrelid), a.attidentity::text, a.attgenerated::text
			from pg_attribute a left join pg_attrdef d on d.adrelid = a.attrelid and d.adnum = a.attnum
			where a.attrelid = $1 and a.attnum > 0 and not a.attisdropped order by a.attnum`, r.oid)
		if err != nil {
			return nil, err
		}
		for crows.Next() {
			var c column
			var def sql.NullString
			var identity, generated string
			if err := crows.Scan(&c.Name, &c.Type, &c.NotNull, &def, &identity, &generated); err != nil {
				return nil, err
			}
			if def.Valid {
				s := def.String
				c.Default = &s
			}
			c.Generated = generated != ""
			c.Autoincrement = identity != "" || def.Valid && strings.HasPrefix(def.String, "nextval(")
			t.Columns = append(t.Columns, c)
		}
		crows.Close()
		krows, err := db.Query(`select con.conname, con.contype::text,
			(select string_agg(a.attname, chr(31) order by k.i) from unnest(con.conkey) with ordinality k(n, i)
				join pg_attribute a on a.attrelid = con.conrelid and a.attnum = k.n),
			coalesce(con.confrelid::regclass::text, ''),
			(select string_agg(a.attname, chr(31) order by k.i) from unnest(con.confkey) with ordinality k(n, i)
				join pg_attribute a on a.attrelid = con.confrelid and a.attnum = k.n),
			con.confdeltype::text, con.confupdtype::text
			from pg_constraint con where con.conrelid = $1 order by con.conname`, r.oid)
		if err != nil {
			return nil, err
		}
		for krows.Next() {
			var name, typ, ref, del, upd string
			var cols, refCols sql.NullString
			if err := krows.Scan(&name, &typ, &cols, &ref, &refCols, &del, &upd); err != nil {
				return nil, err
			}
			switch typ {
			case "p":
				t.PrimaryKey = split(cols)
			case "u":
				t.Uniques = append(t.Uniques, unique{name, split(cols)})
			case "f":
				t.ForeignKeys = append(t.ForeignKeys, foreignKey{name, split(cols), strings.Trim(ref, `"`), split(refCols), pgActions[del], pgActions[upd]})
			case "c":
				t.Checks++
			}
		}
		krows.Close()
		irows, err := db.Query(`select ic.relname, i.indisunique, i.indpred is not null, am.amname,
			(select string_agg(coalesce(a.attname, ''), chr(31) order by k.i) from unnest(i.indkey::int2[]) with ordinality k(n, i)
				left join pg_attribute a on a.attrelid = i.indrelid and a.attnum = k.n where k.i <= i.indnkeyatts),
			(select string_agg((o.v & 1)::text, chr(31) order by o.i) from unnest(i.indoption::int2[]) with ordinality o(v, i) where o.i <= i.indnkeyatts)
			from pg_index i join pg_class ic on ic.oid = i.indexrelid join pg_am am on am.oid = ic.relam
			where i.indrelid = $1 and not exists (select 1 from pg_constraint c where c.conindid = i.indexrelid and c.conrelid = i.indrelid)
			order by ic.relname`, r.oid)
		if err != nil {
			return nil, err
		}
		for irows.Next() {
			var ix index
			var cols, desc sql.NullString
			if err := irows.Scan(&ix.Name, &ix.Unique, &ix.Partial, &ix.Method, &cols, &desc); err != nil {
				return nil, err
			}
			ix.Columns = split(cols)
			for _, d := range split(desc) {
				ix.Descending = append(ix.Descending, d == "1")
			}
			t.Indexes = append(t.Indexes, ix)
		}
		irows.Close()
		out.Tables = append(out.Tables, t)
	}
	return out, nil
}

// checkRE finds a CHECK clause in SQLite's CREATE TABLE text.
var checkRE = regexp.MustCompile(`(?i)\bCHECK\s*\(`)

func sqliteCatalog(db *sql.DB) (*catalog, error) {
	out := &catalog{Tables: []table{}}
	trows, err := db.Query(`select name, sql from sqlite_master where type = 'table' and name not like 'sqlite_%' order by name`)
	if err != nil {
		return nil, err
	}
	var names, sqls []string
	for trows.Next() {
		var n, s string
		if err := trows.Scan(&n, &s); err != nil {
			return nil, err
		}
		names, sqls = append(names, n), append(sqls, s)
	}
	trows.Close()
	for i, name := range names {
		t := table{Name: name, SQL: sqls[i], Columns: []column{}, PrimaryKey: []string{}, Uniques: []unique{}, Indexes: []index{}, ForeignKeys: []foreignKey{}}
		t.Checks = len(checkRE.FindAllString(t.SQL, -1))
		q := strings.ReplaceAll(name, `"`, `""`)
		crows, err := db.Query(`select name, type, "notnull", dflt_value, pk, hidden from pragma_table_xinfo("` + q + `") order by cid`)
		if err != nil {
			return nil, err
		}
		pk := map[int]string{}
		for crows.Next() {
			var c column
			var def sql.NullString
			var p, hidden int
			if err := crows.Scan(&c.Name, &c.Type, &c.NotNull, &def, &p, &hidden); err != nil {
				return nil, err
			}
			if def.Valid {
				s := def.String
				c.Default = &s
			}
			c.Generated = hidden == 2 || hidden == 3
			ai := regexp.MustCompile("(?i)[`\"]" + regexp.QuoteMeta(c.Name) + "[`\"]\\s+integer\\s+PRIMARY\\s+KEY\\s+AUTOINCREMENT")
			c.Autoincrement = ai.MatchString(t.SQL)
			if p > 0 {
				pk[p] = c.Name
			}
			t.Columns = append(t.Columns, c)
		}
		crows.Close()
		for k := 1; k <= len(pk); k++ {
			t.PrimaryKey = append(t.PrimaryKey, pk[k])
		}
		irows, err := db.Query(`select name, "unique", origin, partial from pragma_index_list("` + q + `") order by name`)
		if err != nil {
			return nil, err
		}
		type il struct {
			name, origin    string
			unique, partial bool
		}
		var list []il
		for irows.Next() {
			var x il
			if err := irows.Scan(&x.name, &x.unique, &x.origin, &x.partial); err != nil {
				return nil, err
			}
			list = append(list, x)
		}
		irows.Close()
		for _, x := range list {
			if x.origin == "pk" {
				continue
			}
			xrows, err := db.Query(`select coalesce(name, ''), "desc" from pragma_index_xinfo("` + strings.ReplaceAll(x.name, `"`, `""`) + `") where key = 1 order by seqno`)
			if err != nil {
				return nil, err
			}
			ix := index{Name: x.name, Unique: x.unique, Partial: x.partial, Columns: []string{}}
			for xrows.Next() {
				var c string
				var d bool
				if err := xrows.Scan(&c, &d); err != nil {
					return nil, err
				}
				ix.Columns = append(ix.Columns, c)
				ix.Descending = append(ix.Descending, d)
			}
			xrows.Close()
			if x.origin == "u" {
				t.Uniques = append(t.Uniques, unique{"", ix.Columns})
				continue
			}
			t.Indexes = append(t.Indexes, ix)
		}
		frows, err := db.Query(`select id, "table", "from", coalesce("to", ''), on_update, on_delete from pragma_foreign_key_list("` + q + `") order by id, seq`)
		if err != nil {
			return nil, err
		}
		byID := map[int]int{}
		for frows.Next() {
			var id int
			var ref, from, to, upd, del string
			if err := frows.Scan(&id, &ref, &from, &to, &upd, &del); err != nil {
				return nil, err
			}
			k, ok := byID[id]
			if !ok {
				k = len(t.ForeignKeys)
				byID[id] = k
				t.ForeignKeys = append(t.ForeignKeys, foreignKey{RefTable: ref, OnDelete: del, OnUpdate: upd})
			}
			t.ForeignKeys[k].Columns = append(t.ForeignKeys[k].Columns, from)
			t.ForeignKeys[k].RefColumns = append(t.ForeignKeys[k].RefColumns, to)
		}
		frows.Close()
		out.Tables = append(out.Tables, t)
	}
	return out, nil
}
