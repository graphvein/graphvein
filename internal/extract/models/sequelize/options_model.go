package sequelize

import (
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file merges a model's options with the instance's global `define`
// as Sequelize 6.37.8's init does (lib/model.js:665-700, Utils.merge
// lib/utils.js:65-83): a value the model sets (not undefined) replaces
// the global one, except that two object literals merge and two arrays
// concatenate (the model's elements first); the instance's `schema` is
// the default of the model's (row SQ-K2, SQ-O1, SQ-O2).

// optValue is one merged option: the value and the file it is written in.
type optValue struct {
	e    *typescript.ModelExpr
	file string
}

// modelOptions are a model's merged options.
type modelOptions struct {
	vals map[string]optValue
	// indexes are the elements of the merged indexes arrays in order.
	indexes []optValue
	// indexesUnknown says why the indexes array is not read.
	indexesUnknown string
}

// Option keys with a table fact (rows SQ-K2, SQ-O1, SQ-O2), the keys that
// have none (rows SQ-M1, SQ-M2); `name` is read for the association names
// (assoc.go: a model with it has no known singular name).
var (
	factOptionKeys = map[string]bool{
		"tableName": true, "modelName": true, "freezeTableName": true, "underscored": true,
		"timestamps": true, "createdAt": true, "updatedAt": true, "deletedAt": true,
		"paranoid": true, "version": true, "schema": true, "indexes": true,
	}
	noFactOptionKeys = map[string]bool{
		"hooks": true, "scopes": true, "defaultScope": true, "validate": true,
		"getterMethods": true, "setterMethods": true,
		// init's own instance option and the association names.
		"sequelize": true, "name": true,
	}
)

// mergeOptions merges the model's options object over the global define
// (nil when the instance has none). why names the first entry that is not
// read (a spread or a computed key in either object, or a key no row
// reads): the model is then not read (row D).
func mergeOptions(define, model *typescript.ModelExpr, defineFile, modelFile string) (*modelOptions, string) {
	o := &modelOptions{vals: map[string]optValue{}}
	arrays := map[string][]optValue{}
	for _, src := range []struct {
		obj  *typescript.ModelExpr
		file string
	}{{define, defineFile}, {model, modelFile}} {
		if src.obj == nil {
			continue
		}
		for i := range src.obj.Entries {
			en := &src.obj.Entries[i]
			if en.Kind != typescript.ModelProperty && en.Kind != typescript.ModelShorthand && en.Kind != typescript.ModelMethod {
				return nil, "an options entry that is not a plain property (a spread or a computed key)"
			}
			if !factOptionKeys[en.Key] && !noFactOptionKeys[en.Key] {
				return nil, "the option " + en.Key + " is not read"
			}
			v := &en.Value
			if isUndefined(v) {
				continue
			}
			if en.Key == "indexes" {
				if v.Kind != typescript.ModelArray {
					o.indexesUnknown = "the indexes option is not an array literal"
					continue
				}
				var list []optValue
				for j := range v.Elements {
					list = append(list, optValue{&v.Elements[j], src.file})
				}
				// Utils.merge: the later (model) array first, then the
				// earlier (define) array.
				arrays["indexes"] = append(list, arrays["indexes"]...)
				continue
			}
			o.vals[en.Key] = optValue{v, src.file}
		}
	}
	o.indexes = arrays["indexes"]
	return o, ""
}

// get returns a merged option.
func (o *modelOptions) get(key string) (optValue, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// truthy reads an option as JavaScript tests it: absent is def; a
// literal's truthiness; ok false for anything else.
func (o *modelOptions) truthy(key string, def bool) (bool, bool) {
	v, ok := o.vals[key]
	if !ok {
		return def, true
	}
	return jsTruthy(v.e)
}

// jsTruthy is a literal's truthiness.
func jsTruthy(e *typescript.ModelExpr) (bool, bool) {
	switch e.Kind {
	case typescript.ModelBoolean:
		return e.Value == "true", true
	case typescript.ModelNull:
		return false, true
	case typescript.ModelNumber:
		n, ok := jsNumber(e.Value)
		if !ok {
			return false, false
		}
		return n != "0", true
	case typescript.ModelString, typescript.ModelTemplate:
		s, ok := literalString(e)
		return s != "", ok
	}
	return false, false
}
