package sequelize

import (
	"regexp"
	"strings"
)

// This file ports the naming rules Sequelize 6.37.8 uses to derive table,
// column and foreign-key names (row SQ-K2): pluralize,
// singularize and underscore of inflection 1.13.4 (lib/inflection.js, the
// version both corpora lock), which Sequelize calls through
// lib/utils.js:93-104, and Sequelize's own camelize (lib/utils.js:89-91,
// not inflection's). The rules are inflection's own rule tables in its own
// order, not a generic English pluralizer: `Archived` becomes `Archiveds`.
//
// The port is exact for ASCII input only. JavaScript's case-insensitive
// regular expressions (no u flag) fold case through toUpperCase, Go's
// through Unicode simple folding; they differ for some non-ASCII letters
// (the Kelvin sign matches k in Go, not in JavaScript). Every function
// returns ok false for a string with a non-ASCII byte, and the name derived
// from it is then unknown. TestInflection compares the port with
// inflection 1.13.4's own outputs (testdata/inflection-1.13.4.json).

// uncountableWords is inflection's uncountable_words (lib/inflection.js
// 14-308, the entries that are not commented out).
var uncountableWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`accommodation adulthood advertising advice aggression aid air aircraft alcohol anger applause arithmetic assistance athletics
		bacon baggage beef biology blood botany bread butter
		carbon cardboard cash chalk chaos chess crossroads countryside
		dancing deer dignity dirt dust
		economics education electricity engineering enjoyment envy equipment ethics evidence evolution
		fame fiction flour flu food fuel fun furniture
		gallows garbage garlic genetics gold golf gossip gratitude grief guilt gymnastics
		happiness hardware harm hate hatred health heat help homework honesty honey hospitality housework humour hunger hydrogen
		ice importance inflation information innocence iron irony
		jam jewelry judo
		karate knowledge
		lack laughter lava leather leisure lightning linguine linguini linguistics literature litter livestock logic loneliness luck luggage
		macaroni machinery magic management mankind marble mathematics mayonnaise measles methane milk minus money mud music mumps
		nature news nitrogen nonsense nurture nutrition
		obedience obesity oxygen
		pasta patience physics poetry pollution poverty pride psychology publicity punctuation
		quartz
		racism relaxation reliability research respect revenge rice rubbish rum
		safety scenery seafood seaside series shame sheep shopping sleep smoke smoking snow soap software soil spaghetti species steam stuff stupidity sunshine symmetry
		tennis thirst thunder timber traffic transportation trust
		underwear unemployment unity
		validity veal vegetation vegetarianism vengeance violence vitality
		warmth wealth weather welfare wheat wildlife wisdom
		yoga
		zinc zoology`) {
		uncountableWords[w] = true
	}
}

// inflectionRule is one entry of inflection's plural_rules or
// singular_rules: a pattern and its replacement (hasReplacement false for
// the "already plural/singular" entries, which stop the search and keep
// the word). Patterns are inflection's with the i flag; the g flag does not
// matter here (every pattern is anchored, so it matches at most once).
type inflectionRule struct {
	re             *regexp.Regexp
	replacement    string
	hasReplacement bool
}

// The patterns of inflection's regex.plural and regex.singular
// (lib/inflection.js:321-388).
var (
	plMen       = `^(m|wom)en$`
	plPeople    = `(pe)ople$`
	plChildren  = `(child)ren$`
	plTia       = `([ti])a$`
	plAnalyses  = `((a)naly|(b)a|(d)iagno|(p)arenthe|(p)rogno|(s)ynop|(t)he)ses$`
	plDatabases = `(database)s$`
	plDrives    = `(drive)s$`
	plHives     = `(hi|ti)ves$`
	plCurves    = `(curve)s$`
	plLrves     = `([lr])ves$`
	plAves      = `([a])ves$`
	plFoves     = `([^fo])ves$`
	plMovies    = `(m)ovies$`
	plAeiouyies = `([^aeiouy]|qu)ies$`
	plSeries    = `(s)eries$`
	plXes       = `(x|ch|ss|sh)es$`
	plMice      = `([m|l])ice$`
	plBuses     = `(bus)es$`
	plOes       = `(o)es$`
	plShoes     = `(shoe)s$`
	plCrises    = `(cris|ax|test)es$`
	plOctopuses = `(octop|vir)uses$`
	plAliases   = `(alias|canvas|status|campus)es$`
	plSummonses = `^(summons|bonus)es$`
	plOxen      = `^(ox)en`
	plMatrices  = `(matr)ices$`
	plVertices  = `(vert|ind)ices$`
	plFeet      = `^feet$`
	plTeeth     = `^teeth$`
	plGeese     = `^geese$`
	plQuizzes   = `(quiz)zes$`
	plWhereases = `^(whereas)es$`
	plCriteria  = `^(criteri)a$`
	plGenera    = `^genera$`
	plSs        = `ss$`
	plS         = `s$`

	sgMan       = `^(m|wom)an$`
	sgPerson    = `(pe)rson$`
	sgChild     = `(child)$`
	sgDrive     = `(drive)$`
	sgOx        = `^(ox)$`
	sgAxis      = `(ax|test)is$`
	sgOctopus   = `(octop|vir)us$`
	sgAlias     = `(alias|status|canvas|campus)$`
	sgSummons   = `^(summons|bonus)$`
	sgBus       = `(bu)s$`
	sgBuffalo   = `(buffal|tomat|potat)o$`
	sgTium      = `([ti])um$`
	sgSis       = `sis$`
	sgFfe       = `(?:([^f])fe|([lr])f)$`
	sgHive      = `(hi|ti)ve$`
	sgAeiouyy   = `([^aeiouy]|qu)y$`
	sgX         = `(x|ch|ss|sh)$`
	sgMatrix    = `(matr)ix$`
	sgVertex    = `(vert|ind)ex$`
	sgMouse     = `([m|l])ouse$`
	sgFoot      = `^foot$`
	sgTooth     = `^tooth$`
	sgGoose     = `^goose$`
	sgQuiz      = `(quiz)$`
	sgWhereas   = `^(whereas)$`
	sgCriterion = `^(criteri)on$`
	sgGenus     = `^genus$`
	sgS         = `s$`
	sgCommon    = `$`
)

func keep(p string) inflectionRule { return inflectionRule{re: regexp.MustCompile("(?i)" + p)} }

func rule(p, r string) inflectionRule {
	return inflectionRule{re: regexp.MustCompile("(?i)" + p), replacement: r, hasReplacement: true}
}

// pluralRules is inflection's plural_rules (lib/inflection.js:390-454).
var pluralRules = []inflectionRule{
	keep(plMen), keep(plPeople), keep(plChildren), keep(plTia), keep(plAnalyses), keep(plDatabases),
	keep(plDrives), keep(plHives), keep(plCurves), keep(plLrves), keep(plFoves), keep(plAeiouyies),
	keep(plSeries), keep(plMovies), keep(plXes), keep(plMice), keep(plBuses), keep(plOes), keep(plShoes),
	keep(plCrises), keep(plOctopuses), keep(plAliases), keep(plSummonses), keep(plOxen), keep(plMatrices),
	keep(plFeet), keep(plTeeth), keep(plGeese), keep(plQuizzes), keep(plWhereases), keep(plCriteria),
	keep(plGenera),

	rule(sgMan, "$1en"), rule(sgPerson, "$1ople"), rule(sgChild, "$1ren"), rule(sgDrive, "$1s"),
	rule(sgOx, "$1en"), rule(sgAxis, "$1es"), rule(sgOctopus, "$1uses"), rule(sgAlias, "$1es"),
	rule(sgSummons, "$1es"), rule(sgBus, "$1ses"), rule(sgBuffalo, "$1oes"), rule(sgTium, "$1a"),
	rule(sgSis, "ses"), rule(sgFfe, "$1$2ves"), rule(sgHive, "$1ves"), rule(sgAeiouyy, "$1ies"),
	rule(sgMatrix, "$1ices"), rule(sgVertex, "$1ices"), rule(sgX, "$1es"), rule(sgMouse, "$1ice"),
	rule(sgFoot, "feet"), rule(sgTooth, "teeth"), rule(sgGoose, "geese"), rule(sgQuiz, "$1zes"),
	rule(sgWhereas, "$1es"), rule(sgCriterion, "$1a"), rule(sgGenus, "genera"),

	rule(sgS, "s"), rule(sgCommon, "s"),
}

// singularRules is inflection's singular_rules (lib/inflection.js:456-523).
var singularRules = []inflectionRule{
	keep(sgMan), keep(sgPerson), keep(sgChild), keep(sgDrive), keep(sgOx), keep(sgAxis), keep(sgOctopus),
	keep(sgAlias), keep(sgSummons), keep(sgBus), keep(sgBuffalo), keep(sgTium), keep(sgSis), keep(sgFfe),
	keep(sgHive), keep(sgAeiouyy), keep(sgX), keep(sgMatrix), keep(sgMouse), keep(sgFoot), keep(sgTooth),
	keep(sgGoose), keep(sgQuiz), keep(sgWhereas), keep(sgCriterion), keep(sgGenus),

	rule(plMen, "$1an"), rule(plPeople, "$1rson"), rule(plChildren, "$1"), rule(plDatabases, "$1"),
	rule(plDrives, "$1"), rule(plGenera, "genus"), rule(plCriteria, "$1on"), rule(plTia, "$1um"),
	rule(plAnalyses, "$1$2sis"), rule(plHives, "$1ve"), rule(plCurves, "$1"), rule(plLrves, "$1f"),
	rule(plAves, "$1ve"), rule(plFoves, "$1fe"), rule(plMovies, "$1ovie"), rule(plAeiouyies, "$1y"),
	rule(plSeries, "$1eries"), rule(plXes, "$1"), rule(plMice, "$1ouse"), rule(plBuses, "$1"),
	rule(plOes, "$1"), rule(plShoes, "$1"), rule(plCrises, "$1is"), rule(plOctopuses, "$1us"),
	rule(plAliases, "$1"), rule(plSummonses, "$1"), rule(plOxen, "$1"), rule(plMatrices, "$1ix"),
	rule(plVertices, "$1ex"), rule(plFeet, "foot"), rule(plTeeth, "tooth"), rule(plGeese, "goose"),
	rule(plQuizzes, "$1"), rule(plWhereases, "$1"),

	rule(plSs, "ss"), rule(plS, ""),
}

func asciiOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// applyRules is inflection's _apply_rules without an override: an
// uncountable word (compared lower-cased) is kept; otherwise the first rule
// whose pattern matches decides, replacing every match with JavaScript's
// replacement semantics ($n of a group that did not take part is empty).
func applyRules(s string, rules []inflectionRule) (string, bool) {
	if !asciiOnly(s) {
		return "", false
	}
	if uncountableWords[strings.ToLower(s)] {
		return s, true
	}
	for _, r := range rules {
		if !r.re.MatchString(s) {
			continue
		}
		if !r.hasReplacement {
			return s, true
		}
		return replaceJS(r.re, s, r.replacement), true
	}
	return s, true
}

// replaceJS replaces every match of re in s with repl, expanding $1..$9 as
// String.prototype.replace does (an unmatched group gives "").
func replaceJS(re *regexp.Regexp, s, repl string) string {
	var b strings.Builder
	last := 0
	for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(s[last:m[0]])
		for i := 0; i < len(repl); i++ {
			c := repl[i]
			if c == '$' && i+1 < len(repl) && repl[i+1] >= '1' && repl[i+1] <= '9' {
				g := int(repl[i+1] - '0')
				if 2*g+1 < len(m) && m[2*g] >= 0 {
					b.WriteString(s[m[2*g]:m[2*g+1]])
				}
				i++
				continue
			}
			b.WriteByte(c)
		}
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// pluralize is inflection.pluralize (Sequelize's Utils.pluralize).
func pluralize(s string) (string, bool) { return applyRules(s, pluralRules) }

// singularize is inflection.singularize (Sequelize's Utils.singularize).
func singularize(s string) (string, bool) { return applyRules(s, singularRules) }

var upperLetter = regexp.MustCompile(`([A-Z])`)

// underscore is inflection.underscore without all_upper_case (Sequelize's
// Utils.underscore): each upper-case letter gets a leading underscore,
// one leading underscore of each "::" part is dropped, the parts are
// joined with "/" and the result is lower-cased.
func underscore(s string) (string, bool) {
	if !asciiOnly(s) {
		return "", false
	}
	parts := strings.Split(s, "::")
	for i, p := range parts {
		p = upperLetter.ReplaceAllString(p, "_$1")
		parts[i] = strings.TrimPrefix(p, "_")
	}
	return strings.ToLower(strings.Join(parts, "/")), true
}

// jsSpace is the ASCII part of JavaScript's \s and of String.prototype.trim
// (space, tab, line feed, vertical tab, form feed, carriage return).
const jsSpace = " \t\n\v\f\r"

// camelize is Sequelize's own Utils.camelize (lib/utils.js:89-91):
// `str.trim().replace(/[-_\s]+(.)?/g, (m, c) => c.toUpperCase())`. A run of
// separators at the end leaves c undefined, so Sequelize throws a
// TypeError: ok is false then, as for non-ASCII input.
func camelize(s string) (string, bool) {
	if !asciiOnly(s) {
		return "", false
	}
	s = strings.Trim(s, jsSpace)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '-' && c != '_' && !strings.ContainsRune(jsSpace, rune(c)) {
			b.WriteByte(c)
			continue
		}
		for i < len(s) && (s[i] == '-' || s[i] == '_' || strings.ContainsRune(jsSpace, rune(s[i]))) {
			i++
		}
		if i == len(s) {
			return "", false
		}
		b.WriteString(strings.ToUpper(s[i : i+1]))
	}
	return b.String(), true
}
