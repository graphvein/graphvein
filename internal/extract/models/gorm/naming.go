package gorm

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// This file ports GORM's default naming strategy (gorm.io/gorm v1.31.2
// schema/naming.go, row GO-K4): toDBName (naming.go:131-188, with the
// initialism replacer of naming.go:119-129), TableName (44-49),
// ColumnName (62-64), JoinTableName (67-76), IndexName, UniqueName and
// RelationshipFKName through formatName (79-115). TestNaming compares the
// port with the outputs of the real NamingStrategy.

// commonInitialisms is naming.go:120's list; the replacer turns each into
// its title case ("HTTP" -> "Http") before snake-casing, so "OIDCID" is
// "o_id_c_id".
var commonInitialisms = []string{"API", "ASCII", "CPU", "CSS", "DNS", "EOF", "GUID", "HTML", "HTTP", "HTTPS", "ID", "IP", "JSON", "LHS", "QPS", "RAM", "RHS", "RPC", "SLA", "SMTP", "SSH", "TLS", "TTL", "UID", "UI", "UUID", "URI", "URL", "UTF8", "VM", "XML", "XSRF", "XSS"}

var initialismReplacer = func() *strings.Replacer {
	var pairs []string
	for _, i := range commonInitialisms {
		pairs = append(pairs, i, i[:1]+strings.ToLower(i[1:]))
	}
	return strings.NewReplacer(pairs...)
}()

// toDBName is the default strategy's toDBName for an ASCII name. GORM's
// loop adds 32 to a byte it takes for upper case and writes runes byte by
// byte; the callers refuse non-ASCII names (asciiName) instead of porting
// what that does to them.
func toDBName(name string) string {
	if name == "" {
		return ""
	}
	value := initialismReplacer.Replace(name)
	upper := func(b byte) bool { return b >= 'A' && b <= 'Z' }
	var buf strings.Builder
	var lastCase, nextCase, nextNumber bool
	curCase := upper(value[0])
	for i := 0; i < len(value)-1; i++ {
		v := value[i]
		nextCase = upper(value[i+1])
		nextNumber = value[i+1] >= '0' && value[i+1] <= '9'
		if curCase {
			if !(lastCase && (nextCase || nextNumber)) && i > 0 && value[i-1] != '_' && value[i+1] != '_' {
				buf.WriteByte('_')
			}
			buf.WriteByte(v + 32)
		} else {
			buf.WriteByte(v)
		}
		lastCase = curCase
		curCase = nextCase
	}
	last := value[len(value)-1]
	if curCase {
		if !lastCase && len(value) > 1 {
			buf.WriteByte('_')
		}
		buf.WriteByte(last + 32)
	} else {
		buf.WriteByte(last)
	}
	return buf.String()
}

// asciiName reports a name the port computes: ASCII only (GO-K4: a
// non-ASCII identifier makes the names derived from it unknown).
func asciiName(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// namer is a literal naming strategy (the zero Naming is GORM's default)
// with the identifier limit the dialector applies.
type namer struct {
	Naming
	// limit is the identifier limit formatName uses: the literal
	// IdentifierMaxLength, else 63 under the postgres dialector
	// (postgres.go:39, 53-72) and 64 otherwise (gorm.go:179).
	limit int
}

// newNamer returns the strategy a dialect applies to a literal naming.
func newNamer(n Naming, dialect string) namer {
	limit := n.IdentifierMaxLength
	if limit <= 0 {
		limit = 64
		if dialect == DialectPostgres {
			limit = 63
		}
	}
	return namer{Naming: n, limit: limit}
}

func (n namer) dbName(s string) string {
	if n.NoLowerCase {
		return s
	}
	return toDBName(s)
}

// TableName is NamingStrategy.TableName.
func (n namer) TableName(s string) string {
	if n.SingularTable {
		return n.TablePrefix + n.dbName(s)
	}
	return n.TablePrefix + Plural(n.dbName(s))
}

// ColumnName is NamingStrategy.ColumnName.
func (n namer) ColumnName(s string) string { return n.dbName(s) }

// JoinTableName is NamingStrategy.JoinTableName: a lower-case name is
// kept (with the prefix) unless NoLowerCase.
func (n namer) JoinTableName(s string) string {
	if !n.NoLowerCase && strings.ToLower(s) == s {
		return n.TablePrefix + s
	}
	return n.TableName(s)
}

// IndexName is NamingStrategy.IndexName.
func (n namer) IndexName(table, column string) string {
	return n.formatName("idx", table, n.dbName(column))
}

// UniqueName is NamingStrategy.UniqueName.
func (n namer) UniqueName(table, column string) string {
	return n.formatName("uni", table, n.dbName(column))
}

// RelationshipFKName is NamingStrategy.RelationshipFKName for a relation
// of the schema whose table is table.
func (n namer) RelationshipFKName(table, relation string) string {
	return n.formatName("fk", table, n.dbName(relation))
}

// formatName joins prefix, table and name with "_", replaces "." by "_"
// and, over the identifier limit (in runes), keeps limit-8 bytes and the
// first 8 hex digits of the name's sha1.
func (n namer) formatName(prefix, table, name string) string {
	f := strings.ReplaceAll(strings.Join([]string{prefix, table, name}, "_"), ".", "_")
	if utf8.RuneCountInString(f) > n.limit {
		h := sha1.Sum([]byte(f))
		f = f[:n.limit-8] + hex.EncodeToString(h[:])[:8]
	}
	return f
}
