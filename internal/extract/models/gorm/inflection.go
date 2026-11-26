package gorm

import (
	"regexp"
	"strings"
)

// This file ports github.com/jinzhu/inflection v1.0.0 (MIT licence,
// inflections.go), the version gorm.io/gorm pins, for the default naming
// strategy's table and join-table names (row GO-K4, open point O4: a port
// with a table test against the real library, no new dependency). The
// rules, their order and the compiled forms are the library's; only the
// mutators (AddPlural, AddIrregular, ...) are not ported: a module that
// calls one makes every plural-derived name unknown (Module.InflectionChanged).
// TestInflection compares the port with the library's own outputs.

type inflectionRule struct {
	re      *regexp.Regexp
	replace string
}

type regularRule struct{ find, replace string }

var pluralRules = []regularRule{
	{"([a-z])$", "${1}s"},
	{"s$", "s"},
	{"^(ax|test)is$", "${1}es"},
	{"(octop|vir)us$", "${1}i"},
	{"(octop|vir)i$", "${1}i"},
	{"(alias|status)$", "${1}es"},
	{"(bu)s$", "${1}ses"},
	{"(buffal|tomat)o$", "${1}oes"},
	{"([ti])um$", "${1}a"},
	{"([ti])a$", "${1}a"},
	{"sis$", "ses"},
	{"(?:([^f])fe|([lr])f)$", "${1}${2}ves"},
	{"(hive)$", "${1}s"},
	{"([^aeiouy]|qu)y$", "${1}ies"},
	{"(x|ch|ss|sh)$", "${1}es"},
	{"(matr|vert|ind)(?:ix|ex)$", "${1}ices"},
	{"^(m|l)ouse$", "${1}ice"},
	{"^(m|l)ice$", "${1}ice"},
	{"^(ox)$", "${1}en"},
	{"^(oxen)$", "${1}"},
	{"(quiz)$", "${1}zes"},
}

var singularRules = []regularRule{
	{"s$", ""},
	{"(ss)$", "${1}"},
	{"(n)ews$", "${1}ews"},
	{"([ti])a$", "${1}um"},
	{"((a)naly|(b)a|(d)iagno|(p)arenthe|(p)rogno|(s)ynop|(t)he)(sis|ses)$", "${1}sis"},
	{"(^analy)(sis|ses)$", "${1}sis"},
	{"([^f])ves$", "${1}fe"},
	{"(hive)s$", "${1}"},
	{"(tive)s$", "${1}"},
	{"([lr])ves$", "${1}f"},
	{"([^aeiouy]|qu)ies$", "${1}y"},
	{"(s)eries$", "${1}eries"},
	{"(m)ovies$", "${1}ovie"},
	{"(c)ookies$", "${1}ookie"},
	{"(x|ch|ss|sh)es$", "${1}"},
	{"^(m|l)ice$", "${1}ouse"},
	{"(bus)(es)?$", "${1}"},
	{"(o)es$", "${1}"},
	{"(shoe)s$", "${1}"},
	{"(cris|test)(is|es)$", "${1}is"},
	{"^(a)x[ie]s$", "${1}xis"},
	{"(octop|vir)(us|i)$", "${1}us"},
	{"(alias|status)(es)?$", "${1}"},
	{"^(ox)en", "${1}"},
	{"(vert|ind)ices$", "${1}ex"},
	{"(matr)ices$", "${1}ix"},
	{"(quiz)zes$", "${1}"},
	{"(database)s$", "${1}"},
}

var irregularRules = [][2]string{
	{"person", "people"},
	{"man", "men"},
	{"child", "children"},
	{"sex", "sexes"},
	{"move", "moves"},
	{"mombie", "mombies"},
}

var uncountableWords = []string{"equipment", "information", "rice", "money", "species", "series", "fish", "sheep", "jeans", "police"}

// pluralMaps and singularMaps are the compiled rules in the order the
// library's compile() builds them: uncountables, irregulars, then the
// regular rules from last to first, each as upper case, as written and
// case-insensitive.
var pluralMaps, singularMaps = compileInflections()

// title upper-cases the first letter, as strings.Title does for the
// library's ASCII words.
func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func compileInflections() ([]inflectionRule, []inflectionRule) {
	var plural, singular []inflectionRule
	for _, u := range uncountableWords {
		r := inflectionRule{regexp.MustCompile("^(?i)(" + u + ")$"), "${1}"}
		plural = append(plural, r)
		singular = append(singular, r)
	}
	for _, v := range irregularRules {
		s, p := v[0], v[1]
		plural = append(plural,
			inflectionRule{regexp.MustCompile(strings.ToUpper(s) + "$"), strings.ToUpper(p)},
			inflectionRule{regexp.MustCompile(title(s) + "$"), title(p)},
			inflectionRule{regexp.MustCompile(s + "$"), p})
	}
	for _, v := range irregularRules {
		s, p := v[0], v[1]
		singular = append(singular,
			inflectionRule{regexp.MustCompile(strings.ToUpper(p) + "$"), strings.ToUpper(s)},
			inflectionRule{regexp.MustCompile(title(p) + "$"), title(s)},
			inflectionRule{regexp.MustCompile(p + "$"), s})
	}
	add := func(out []inflectionRule, rules []regularRule) []inflectionRule {
		for i := len(rules) - 1; i >= 0; i-- {
			v := rules[i]
			out = append(out,
				inflectionRule{regexp.MustCompile(strings.ToUpper(v.find)), strings.ToUpper(v.replace)},
				inflectionRule{regexp.MustCompile(v.find), v.replace},
				inflectionRule{regexp.MustCompile("(?i)" + v.find), v.replace})
		}
		return out
	}
	return add(plural, pluralRules), add(singular, singularRules)
}

func inflect(maps []inflectionRule, s string) string {
	for _, r := range maps {
		if r.re.MatchString(s) {
			return r.re.ReplaceAllString(s, r.replace)
		}
	}
	return s
}

// Plural is inflection.Plural of jinzhu/inflection v1.0.0.
func Plural(s string) string { return inflect(pluralMaps, s) }

// Singular is inflection.Singular of jinzhu/inflection v1.0.0 (GORM's
// many2many self-join column names use it, row GO-R2).
func Singular(s string) string { return inflect(singularMaps, s) }
