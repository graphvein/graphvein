package gorm

import (
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file ports GORM's relation parse (gorm.io/gorm v1.31.2
// schema/relationship.go): parseRelation (65-131), guessRelation
// (456-622, row GO-R1), buildMany2ManyRelation (271-444, row GO-R2) and
// ParseConstraint (661-726, row GO-R3). The parse is eager: every relation
// of every schema reached is parsed once, as GORM's schema cache does,
// and its order-dependent effects (a has-relation registering on the
// child, a referenced field's type copied onto the foreign key) are kept
// with the schema that caused them, so the migrate simulation (migrate.go)
// can decide which of them a table saw when it was created.

// Relation kinds (schema.RelationshipType).
const (
	relBelongsTo  = "belongs_to"
	relHasOne     = "has_one"
	relHasMany    = "has_many"
	relMany2Many  = "many_to_many"
	relPolymorphc = "polymorphic"
)

// grel is one relation (schema.Relationship).
type grel struct {
	// Name is the relation's name: the field name, or for a join table's
	// own belongs-to relations the schema name (relationship.go:375-399).
	Name string
	// Field is the relation field (for a join table's relations, the
	// many2many field).
	Field               *gfield
	Schema, FieldSchema *gschema
	Kind                string
	Refs                []gref
	// Join is a many2many relation's join table schema.
	Join *gschema
	// Unread says why the relation is not read (row D): a polymorphic
	// relation or a relation field of an embedded struct; polyID is a
	// polymorphic relation's id field on the target.
	Unread, polyID string
}

// gref is one reference of a relation (schema.Reference).
type gref struct {
	PK, FK *gfield
	Own    bool
}

// typeCopy is one copy of a referenced field's data type onto a
// foreign-key field: Owner is the schema whose relation parse made it
// (the copy exists once Owner is parsed), Before the field's state before
// the first copy and Changed whether this copy changed it.
type typeCopy struct {
	Owner   *gschema
	Before  fieldState
	Changed bool
}

// fieldState is the part of a field a type copy changes.
type fieldState struct {
	DataType, GORMDataType string
	Size                   int
}

func (f *gfield) state() fieldState {
	return fieldState{DataType: f.DataType, GORMDataType: f.GORMDataType, Size: f.Size}
}

// copyable is relationship.go:775-783.
func copyable(dataType string) bool {
	l := strings.ToLower(dataType)
	return !strings.Contains(l, "auto_increment") && !strings.Contains(l, "primary key")
}

// copyType copies the referenced field's data type onto a foreign-key
// field (relationship.go:592-604, 437-442 for join fields).
func copyType(fk, pk *gfield, owner *gschema) {
	before := fk.state()
	if copyable(pk.DataType) {
		fk.DataType = pk.DataType
	}
	fk.GORMDataType = pk.GORMDataType
	if fk.Size == 0 {
		fk.Size = pk.Size
	}
	if len(fk.copies) > 0 {
		before = fk.copies[0].Before
	}
	fk.copies = append(fk.copies, typeCopy{Owner: owner, Before: before, Changed: fk.state() != before})
}

// lookUpField is Schema.LookUpField (schema.go:79-97): by column name, by
// field name, then by the namer's column name of the name.
func (p *porter) lookUpField(sc *gschema, name string) *gfield {
	if f := sc.lookUp(name); f != nil {
		return f
	}
	if asciiName(name) {
		return sc.ByDB[p.namer.ColumnName(name)]
	}
	return nil
}

// toColumns is utils.go:45-52.
func toColumns(v string) []string {
	var out []string
	if v != "" {
		for _, s := range strings.Split(v, ",") {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// relations parses the relation fields of a top-level schema
// (schema.go:383-389, parseRelation); a relation GORM cannot build fails
// the schema (GO-P1).
func (p *porter) relations(sc *gschema) {
	for _, f := range sc.Relations {
		if sc.Failure != "" {
			return
		}
		p.relation(sc, f)
	}
}

// relation ports parseRelation for one field.
func (p *porter) relation(sc *gschema, f *gfield) {
	t := f.Type
	slice := false
	if t.Kind == "slice" && t.Elem != nil {
		t = *t.Elem
		slice = true
	}
	if t.Kind != "struct" || t.Struct == nil {
		// GB3's parseField already failed a non-struct relation field.
		return
	}
	rel := &grel{Name: f.Name, Field: f, Schema: sc}
	if t.Struct.Model {
		rel.Unread = "the relation " + f.Name + " targets gorm.Model"
		sc.Rels = append(sc.Rels, rel)
		sc.unreadRelation(rel, "")
		return
	}
	fs := p.parse(t.Struct, false, "", f.Line, 0)
	rel.FieldSchema = fs
	if fs.Failure != "" {
		sc.fail(f.Line, "the relation "+sc.Name+"."+f.Name+" targets "+fs.Name+", which GORM cannot parse: "+fs.Failure)
		return
	}
	foreignKeys, primaryKeys := toColumns(f.Tags["FOREIGNKEY"]), toColumns(f.Tags["REFERENCES"])
	_, polymorphic := f.Tags["POLYMORPHIC"]
	_, pType := f.Tags["POLYMORPHICTYPE"]
	_, pID := f.Tags["POLYMORPHICID"]
	switch {
	case len(f.BindNames) > 1:
		rel.Unread = "the relation " + f.Name + " is a field of an embedded struct (guessEmbeddedBelongs and guessEmbeddedHas are not ported)"
	case polymorphic || pType && pID:
		p.polymorphic(sc, rel, f)
	case f.Tags["MANY2MANY"] != "":
		p.many2many(sc, rel, f, f.Tags["MANY2MANY"], foreignKeys, primaryKeys)
	case f.Tags["BELONGSTO"] != "":
		p.guess(sc, rel, f, guessBelongs, foreignKeys, primaryKeys)
	case !slice:
		p.guess(sc, rel, f, guessGuess, foreignKeys, primaryKeys)
	default:
		p.guess(sc, rel, f, guessHas, foreignKeys, primaryKeys)
	}
	if sc.Failure != "" {
		return
	}
	if rel.Unread != "" {
		sc.Rels = append(sc.Rels, rel)
		sc.unreadRelation(rel, rel.polyID)
		return
	}
	if rel.Kind == "has" {
		rel.Kind = relHasOne
		if slice {
			rel.Kind = relHasMany
		}
		if fs != sc {
			fs.Registered = append(fs.Registered, rel)
		}
	}
	sc.Rels = append(sc.Rels, rel)
}

// unreadRelation records a relation the port does not read (row D): the
// foreign keys of the schemas it may put a constraint on are unknown, and
// the fields it may pick as foreign keys keep no type (GORM copies the
// referenced type onto them): a polymorphic relation's id field on the
// target; for an embedded relation, the fields named after the relation
// or either struct, or in its foreignKey tag, on either side.
func (sc *gschema) unreadRelation(rel *grel, polyID string) {
	why := rel.Unread
	fs := rel.FieldSchema
	if polyID == "" {
		sc.FKWhy = why
	}
	if fs == nil {
		return
	}
	fs.FKWhy = why
	for _, s := range []*gschema{sc, fs} {
		if s.TypeWhy == nil {
			s.TypeWhy = map[*gfield]string{}
		}
		for _, x := range s.Fields {
			switch {
			case polyID != "":
				if s == fs && x.Name == polyID {
					s.TypeWhy[x] = why
				}
			case strings.HasPrefix(x.Name, rel.Field.Name) || strings.HasPrefix(x.Name, sc.Name) || strings.HasPrefix(x.Name, fs.Name):
				s.TypeWhy[x] = why
			default:
				for _, k := range toColumns(rel.Field.Tags["FOREIGNKEY"]) {
					if x.Name == k || x.DBName == k {
						s.TypeWhy[x] = why
					}
				}
			}
		}
	}
}

// polymorphic checks a polymorphic relation's fields (relationship.go:
// 189-269): a missing type or id field fails the schema; otherwise the
// relation is not read (row D).
func (p *porter) polymorphic(sc *gschema, rel *grel, f *gfield) {
	poly := f.Tags["POLYMORPHIC"]
	typeName, typeID := poly+"Type", poly+"ID"
	if v, ok := f.Tags["POLYMORPHICTYPE"]; ok {
		typeName = strings.TrimSpace(v)
	}
	if v, ok := f.Tags["POLYMORPHICID"]; ok {
		typeID = strings.TrimSpace(v)
	}
	if rel.FieldSchema.ByName[typeName] == nil || rel.FieldSchema.ByName[typeID] == nil {
		sc.fail(f.Line, "the polymorphic relation "+sc.Name+"."+f.Name+" names a type or id field "+rel.FieldSchema.Name+" does not have")
		return
	}
	rel.Kind = relPolymorphc
	rel.polyID = typeID
	rel.Unread = "the polymorphic relation " + sc.Name + "." + f.Name + " is not read"
}

// Guess levels (relationship.go:446-454).
const (
	guessGuess = iota
	guessBelongs
	guessEmbeddedBelongs
	guessHas
	guessEmbeddedHas
)

// guess ports guessRelation. Embedded relations are not read, so a field
// never has an owner schema and the embedded levels re-guess at once.
func (p *porter) guess(sc *gschema, rel *grel, f *gfield, cgl int, foreignKeys, primaryKeys []string) {
	fs := rel.FieldSchema
	gl := cgl
	if gl == guessGuess {
		if sc == fs {
			gl = guessBelongs
		} else {
			gl = guessHas
		}
	}
	reguess := func() {
		switch cgl {
		case guessGuess:
			p.guess(sc, rel, f, guessBelongs, foreignKeys, primaryKeys)
		case guessBelongs:
			p.guess(sc, rel, f, guessEmbeddedBelongs, foreignKeys, primaryKeys)
		case guessEmbeddedBelongs:
			p.guess(sc, rel, f, guessHas, foreignKeys, primaryKeys)
		case guessHas:
			p.guess(sc, rel, f, guessEmbeddedHas, foreignKeys, primaryKeys)
		default:
			sc.fail(f.Line, "GORM finds no foreign key for the relation "+sc.Name+"."+f.Name+" (relationship.go:476-490)")
		}
	}
	primarySchema, foreignSchema := sc, fs
	switch gl {
	case guessBelongs:
		primarySchema, foreignSchema = fs, sc
	case guessEmbeddedBelongs, guessEmbeddedHas:
		reguess()
		return
	}
	var primaryFields, foreignFields []*gfield
	if len(foreignKeys) > 0 {
		for _, k := range foreignKeys {
			x := p.lookUpField(foreignSchema, k)
			if x == nil {
				reguess()
				return
			}
			foreignFields = append(foreignFields, x)
		}
	} else {
		name := primarySchema.Name
		if len(primaryKeys) > 0 {
			for _, k := range primaryKeys {
				if x := p.lookUpField(primarySchema, k); x != nil {
					primaryFields = append(primaryFields, x)
				}
			}
		} else {
			primaryFields = append(primaryFields, primarySchema.Primary...)
		}
		n := len(primaryFields)
	loop:
		for i := 0; i < n; i++ {
			pf := primaryFields[i]
			lookUp := name + pf.Name
			if gl == guessBelongs {
				lookUp = f.Name + pf.Name
			}
			names := []string{lookUp}
			if len(primaryFields) == 1 {
				base := strings.TrimSuffix(lookUp, pf.Name)
				names = append(names, base+"ID", base+"Id", p.namer.ColumnName(base+"ID"))
			}
			// LookUpFieldByBindName never matches a top-level field
			// (schema.go:107-115): the plain lookup decides.
			for _, nm := range names {
				if x := p.lookUpField(foreignSchema, nm); x != nil {
					foreignFields = append(foreignFields, x)
					primaryFields = append(primaryFields, pf)
					continue loop
				}
			}
		}
	}
	switch {
	case len(foreignFields) == 0:
		reguess()
		return
	case len(primaryKeys) > 0:
		for idx, k := range primaryKeys {
			x := p.lookUpField(primarySchema, k)
			switch {
			case x == nil:
				reguess()
				return
			case len(primaryFields) < idx+1:
				primaryFields = append(primaryFields, x)
			case x != primaryFields[idx]:
				reguess()
				return
			}
		}
	case len(primaryFields) == 0:
		switch {
		case len(foreignFields) == 1 && primarySchema.Prioritized != nil:
			primaryFields = append(primaryFields, primarySchema.Prioritized)
		case len(primarySchema.Primary) == len(foreignFields):
			primaryFields = append(primaryFields, primarySchema.Primary...)
		default:
			reguess()
			return
		}
	}
	if len(primaryFields) < len(foreignFields) {
		sc.fail(f.Line, "the relation "+sc.Name+"."+f.Name+" has more foreign keys than referenced fields")
		return
	}
	rel.Refs = nil
	for idx, ff := range foreignFields {
		pf := primaryFields[idx]
		if ff.DBName == "" || pf.DBName == "" {
			rel.Unread = "the relation " + sc.Name + "." + f.Name + " pairs a field that has no column"
			return
		}
		copyType(ff, pf, sc)
		rel.Refs = append(rel.Refs, gref{PK: pf, FK: ff, Own: sc == primarySchema && gl == guessHas})
	}
	if gl == guessHas {
		rel.Kind = "has"
	} else {
		rel.Kind = relBelongsTo
	}
}

// joinTitle is cases.Title(language.Und, cases.NoLower) on a Go
// identifier: the first letter upper-cased. ok is false for a string
// with another character, whose word breaks are not ported.
func joinTitle(s string) (string, bool) {
	for _, r := range s {
		if !(r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')) {
			return "", false
		}
	}
	if s == "" {
		return "", false
	}
	return strings.ToUpper(s[:1]) + s[1:], true
}

// appendSetting and removeSetting are utils.go:54-67 on a field's gorm tag.
func appendSetting(tag reflect.StructTag, value string) reflect.StructTag {
	t := tag.Get("gorm")
	if strings.Contains(t, value) {
		return tag
	}
	return reflect.StructTag(`gorm:"` + value + ";" + t + `"`)
}

var removeSettingRes = map[string]*regexp.Regexp{}

func removeSetting(tag reflect.StructTag, names ...string) reflect.StructTag {
	for _, name := range names {
		re := removeSettingRes[name]
		if re == nil {
			re = regexp.MustCompile(`(?i)(gorm:.*?)(` + name + `(:.*?)?)(;|("))`)
			removeSettingRes[name] = re
		}
		tag = reflect.StructTag(re.ReplaceAllString(string(tag), "${1}${5}"))
	}
	return tag
}

// joinField is one field of a join table being built.
type joinField struct {
	name string
	from *gfield
}

// many2many ports buildMany2ManyRelation: the join table's fields from
// the two sides' keys (types copied from them), its name through
// JoinTableName, every field a primary-key member, and its two belongs-to
// relations whose constraints carry the many2many field's constraint tag.
func (p *porter) many2many(sc *gschema, rel *grel, f *gfield, name string, foreignKeys, primaryKeys []string) {
	fs := rel.FieldSchema
	rel.Kind = relMany2Many
	joinFK, joinRefs := toColumns(f.Tags["JOINFOREIGNKEY"]), toColumns(f.Tags["JOINREFERENCES"])
	own := append([]*gfield(nil), sc.Primary...)
	refs := append([]*gfield(nil), fs.Primary...)
	if len(foreignKeys) > 0 {
		own = nil
		for _, k := range foreignKeys {
			x := p.lookUpField(sc, k)
			if x == nil {
				sc.fail(f.Line, "the many2many relation "+sc.Name+"."+f.Name+" names the foreign key "+k+", which "+sc.Name+" does not have")
				return
			}
			own = append(own, x)
		}
	}
	if len(primaryKeys) > 0 {
		refs = nil
		for _, k := range primaryKeys {
			x := p.lookUpField(fs, k)
			if x == nil {
				sc.fail(f.Line, "the many2many relation "+sc.Name+"."+f.Name+" names the reference "+k+", which "+fs.Name+" does not have")
				return
			}
			refs = append(refs, x)
		}
	}
	title := func(s string) string {
		t, ok := joinTitle(s)
		if !ok {
			rel.Unread = "the join table name parts of " + sc.Name + "." + f.Name + " are not computed"
		}
		return t
	}
	var fields []joinField
	ownMap, refMap, all := map[string]*gfield{}, map[string]*gfield{}, map[string]*gfield{}
	for idx, of := range own {
		n := title(sc.Name) + of.Name
		if len(joinFK) > idx {
			n = title(joinFK[idx])
		}
		ownMap[n], all[n] = of, of
		fields = append(fields, joinField{name: n, from: of})
	}
	for idx, rf := range refs {
		n := title(fs.Name) + rf.Name
		if _, ok := ownMap[n]; ok {
			if f.Name != fs.Name {
				if p.inflection != "" {
					rel.Unread = "the join column name of " + sc.Name + "." + f.Name + " is a singular and " + p.inflection
				}
				n = Singular(f.Name) + rf.Name
			} else {
				n += "Reference"
			}
		}
		if len(joinRefs) > idx {
			n = title(joinRefs[idx])
		}
		refMap[n] = rf
		if _, ok := all[n]; !ok {
			all[n] = rf
			fields = append(fields, joinField{name: n, from: rf})
		}
	}
	if rel.Unread != "" {
		return
	}
	join := &gschema{Name: "", ByDB: map[string]*gfield{}, ByName: map[string]*gfield{}, Dup: map[string]bool{}, Join: true, JoinOf: rel}
	join.Ref = &structRef{File: f.File, Dir: sc.Ref.Dir, Struct: &facts.GoStruct{Name: sc.Name + "." + f.Name, Line: f.Line, EndLine: f.Line}}
	var raws []rawField
	for _, jf := range fields {
		tag := removeSetting(appendSetting(reflect.StructTag(`gorm:"`+jf.from.Raw+`"`), "primaryKey"), "column", "autoincrement", "index", "unique", "uniqueindex")
		raws = append(raws, rawField{name: jf.name, file: f.File, line: f.Line, typ: jf.from.Type, tag: string(tag)})
	}
	p.fill(join, raws, false, 0)
	if join.Failure != "" {
		sc.fail(f.Line, "the join table of "+sc.Name+"."+f.Name+" fails: "+join.Failure)
		return
	}
	switch {
	case !asciiName(name):
		join.TableWhy = "the join table name " + name + " is not ASCII"
	case !p.namer.NoLowerCase && strings.ToLower(name) == name:
		join.Table = p.namer.JoinTableName(name)
	case !p.namer.SingularTable && p.inflection != "":
		join.TableWhy = "the join table name is a plural and " + p.inflection
	default:
		join.Table = p.namer.JoinTableName(name)
	}
	relName, relRefName := sc.Name, fs.Name
	if relName == relRefName {
		relRefName = f.Name
	}
	toOwner := &grel{Name: relName, Field: f, Schema: join, FieldSchema: sc, Kind: relBelongsTo}
	toRef := &grel{Name: relRefName, Field: f, Schema: join, FieldSchema: fs, Kind: relBelongsTo}
	join.Primary = nil
	for _, jf := range join.Fields {
		if !(jf.C || jf.R || jf.U) {
			continue
		}
		src := all[jf.Name]
		if src == nil {
			continue
		}
		copyType(jf, src, join)
		jf.joinSrc = src
		jf.PK = true
		join.Primary = append(join.Primary, jf)
		if of, ok := ownMap[jf.Name]; ok {
			toOwner.Refs = append(toOwner.Refs, gref{PK: of, FK: jf})
			rel.Refs = append(rel.Refs, gref{PK: of, FK: jf, Own: true})
		}
		if rf, ok := refMap[jf.Name]; ok {
			toRef.Refs = append(toRef.Refs, gref{PK: rf, FK: jf})
			rel.Refs = append(rel.Refs, gref{PK: rf, FK: jf})
		}
	}
	join.Rels = []*grel{toOwner, toRef}
	rel.Join = join
}

// gcon is one foreign-key constraint (schema.Constraint).
type gcon struct {
	rel *grel
	// Name is the constraint name per dialect (the explicit name, else
	// RelationshipFKName at each dialect's identifier limit).
	Name               map[string]string
	Schema, Ref        *gschema
	FKs, Refs          []*gfield
	OnDelete, OnUpdate string
}

var reConstraintName = regexp.MustCompile(`^[\w-]+$`)

// constraint ports ParseConstraint: nil for constraint:- and for a
// belongs-to relation whose target has the reverse has-relation over the
// same references (that relation's constraint is the one created).
func (b *builder) constraint(rel *grel) *gcon {
	str := rel.Field.Tags["CONSTRAINT"]
	if str == "-" || rel.Unread != "" {
		return nil
	}
	if rel.Kind == relBelongsTo && rel.FieldSchema != nil {
		for _, r := range rel.FieldSchema.Rels {
			if r != rel && r.FieldSchema == rel.Schema && len(r.Refs) == len(rel.Refs) {
				matched := true
				for i, ref := range r.Refs {
					if rel.Refs[i].PK != ref.PK || rel.Refs[i].FK != ref.FK {
						matched = false
						break
					}
				}
				if matched {
					return nil
				}
			}
		}
	}
	settings := parseTagSetting(str, ",")
	c := &gcon{rel: rel, Name: map[string]string{}, OnUpdate: settings["ONUPDATE"], OnDelete: settings["ONDELETE"]}
	if idx := strings.IndexByte(str, ','); idx != -1 && reConstraintName.MatchString(str[:idx]) {
		for _, d := range b.m.Dialects {
			c.Name[d] = str[:idx]
		}
	} else if rel.Schema.Table != "" && asciiName(rel.Name) {
		for _, d := range b.m.Dialects {
			c.Name[d] = b.namer(d).RelationshipFKName(rel.Schema.Table, rel.Name)
		}
	}
	for _, ref := range rel.Refs {
		if ref.PK != nil && (rel.Join == nil || ref.Own) {
			c.FKs = append(c.FKs, ref.FK)
			c.Refs = append(c.Refs, ref.PK)
			if ref.Own {
				c.Schema, c.Ref = ref.FK.Schema, rel.Schema
			} else {
				c.Schema, c.Ref = rel.Schema, ref.PK.Schema
			}
		}
	}
	return c
}
