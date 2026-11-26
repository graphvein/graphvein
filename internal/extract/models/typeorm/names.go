package typeorm

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
)

// This file holds the naming rules of rows TO-K2, TO-K6 and TO-X4 that the
// relations and constraints need: join column, junction table and
// junction column names (DefaultNamingStrategy.js:114-131 at 0.3.31,
// :96-115 at 1.1.3; snake-naming.strategy.js:17-29 at 4.1.0) and the
// generated constraint and index names (DefaultNamingStrategy.js:46-113 /
// :28-94).

// camelCase is TypeORM's camelCase (util/StringUtils.js:16-24 / :17-25):
// `str.replace(/^([A-Z])|[\s-_](\w)/g, ...)`, a leading capital lowered
// and every whitespace, `-` or `_` followed by a word character replaced
// by that character upper-cased.
func camelCase(s string) string {
	var b strings.Builder
	word := func(c byte) bool {
		return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	sep := func(c byte) bool {
		return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v' || c == '-' || c == '_'
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case i == 0 && c >= 'A' && c <= 'Z':
			b.WriteByte(c + ('a' - 'A'))
		case sep(c) && i+1 < len(s) && word(s[i+1]):
			b.WriteString(strings.ToUpper(string(s[i+1])))
			i++
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// joinColumnName is the naming strategy's joinColumnName(relation,
// referencedProperty).
func (c *claimer) joinColumnName(relation, referenced string) string {
	if c.g.Naming == "snake" {
		return snakeCase(relation + "_" + referenced)
	}
	return camelCase(relation + "_" + referenced)
}

// joinTableName is joinTableName(ownerTable, targetTable, property) over
// the unprefixed table names (A18); both strategies snake-case the joined
// text.
func joinTableName(owner, target, property string) string {
	return snakeCase(owner + "_" + strings.ReplaceAll(property, ".", "_") + "_" + target)
}

// joinTableColumnName is joinTableColumnName(table, property, databaseName),
// which prefers the referenced column's database name.
func (c *claimer) joinTableColumnName(table, databaseName string) string {
	if c.g.Naming == "snake" {
		return snakeCase(table + "_" + databaseName)
	}
	return camelCase(table + "_" + databaseName)
}

// nameHash is the hash of a generated name per major line:
// 0.3.x RandomGenerator.sha1 hashes encodeURIComponent(key) as bytes
// (util/RandomGenerator.js:17-43, the unescape commented out), 1.x the
// standard SHA-1 of the key's UTF-8 (util/StringUtils.js:98-99).
func (c *claimer) nameHash(key string) string {
	if c.g.Package.Line != Line1 {
		key = encodeURIComponent(key)
	}
	sum := sha1.Sum([]byte(key))
	return hex.EncodeToString(sum[:])
}

// encodeURIComponent is JavaScript's: every UTF-8 byte except
// A-Z a-z 0-9 - _ . ! ~ * ' ( ) is written %XX (upper-case hex).
func encodeURIComponent(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("-_.!~*'()", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&15])
	}
	return b.String()
}

// generatedName is DefaultNamingStrategy's name of kind prefix ("PK_",
// "UQ_", "REL_", "FK_", "IDX_") for a table and its column names (sorted,
// joined by "_", with "_" + where appended for REL_ and IDX_ when where is
// set): the prefix and the first 27 hex digits of the hash (26 for REL_
// and IDX_). The table name's first "." becomes "_" (getTableName drops a
// schema first).
func (c *claimer) generatedName(prefix, table string, columns []string, where string) string {
	sorted := append([]string(nil), columns...)
	sort.Strings(sorted)
	key := strings.Replace(table, ".", "_", 1) + "_" + strings.Join(sorted, "_")
	if where != "" {
		key += "_" + where
	}
	n := 27
	if prefix == "REL_" || prefix == "IDX_" {
		n = 26
	}
	return prefix + c.nameHash(key)[:n]
}

// checkName is checkConstraintName(table, expression): "CHK_" and 26 hex
// digits of the hash of "<table>_<expression>".
func (c *claimer) checkName(table, expression string) string {
	return "CHK_" + c.nameHash(strings.Replace(table, ".", "_", 1) + "_" + expression)[:26]
}

// identitySegment makes a model or column name safe for a node identity,
// whose part after "#" is written unescaped only when it holds A-Z a-z
// 0-9 . _ ~ - (internal/contract Identity; the kit writes the model name
// as the qualified name): every other byte becomes "_". An entity two
// groups reach is the model "<Class>~<group root>", a junction table its
// table name; two names that differ only there would collide, which the
// kit reports as a duplicate record.
func identitySegment(s string) string {
	b := []byte(s)
	for i, c := range b {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("._~-", c) >= 0) {
			b[i] = '_'
		}
	}
	return string(b)
}
