package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	schemas "github.com/graphvein/graphvein/docs/contract/v1"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var loadSchemas = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(embeddedOnly{})
	compiler.AssertFormat()
	names := []string{"header", "file", "node", "edge", "diagnostic", "footer"}
	for _, name := range names {
		data, err := schemas.Files.ReadFile(name + ".schema.json")
		if err != nil {
			return nil, err
		}
		var obj any
		if err := json.Unmarshal(data, &obj); err != nil {
			return nil, err
		}
		if err := compiler.AddResource(name+".schema.json", obj); err != nil {
			return nil, err
		}
	}
	result := make(map[string]*jsonschema.Schema)
	for _, name := range names {
		schema, err := compiler.Compile(name + ".schema.json")
		if err != nil {
			return nil, err
		}
		result[name] = schema
	}
	return result, nil
})

type embeddedOnly struct{}

func (embeddedOnly) Load(url string) (any, error) {
	return nil, fmt.Errorf("schema resource %q is not embedded", url)
}

// Validate checks the published structural schemas and canonical graph semantics.
func Validate(record any) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encoding record: %w", err)
	}
	_, _, err = decode(data, true)
	return err
}

// Decode ignores extension fields but reports their JSON paths for diagnostics.
func Decode(data []byte) (any, []string, error) {
	return decode(data, false)
}

func decode(data []byte, explain bool) (any, []string, error) {
	if !utf8.Valid(data) {
		return nil, nil, fmt.Errorf("invalid UTF-8 record")
	}
	if err := uniqueKeys(data); err != nil {
		return nil, nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("invalid JSON object")
	}
	var typ string
	if err := json.Unmarshal(raw["type"], &typ); err != nil {
		return nil, nil, fmt.Errorf("missing record type")
	}
	if typ == "header" {
		var version int
		_ = json.Unmarshal(raw["contract_version"], &version)
		if version != Version {
			return nil, nil, fmt.Errorf("unsupported contract_version %d; supported: %d", version, Version)
		}
	}
	var value any
	switch typ {
	case "header":
		value = &Header{}
	case "file":
		value = &File{}
	case "node":
		value = &Node{}
	case "edge":
		value = &Edge{}
	case "diagnostic":
		value = &Diagnostic{}
	case "footer":
		value = &Footer{}
	default:
		return nil, nil, fmt.Errorf("unknown record type %q", typ)
	}
	compiled, err := loadSchemas()
	if err != nil {
		return nil, nil, fmt.Errorf("loading contract schemas: %w", err)
	}
	var obj any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&obj); err != nil {
		return nil, nil, fmt.Errorf("invalid JSON object")
	}
	if err := compiled[typ].Validate(obj); err != nil {
		if explain {
			if validation, ok := err.(*jsonschema.ValidationError); ok {
				for len(validation.Causes) > 0 {
					validation = validation.Causes[0]
				}
				return nil, nil, fmt.Errorf("invalid %s record: schema validation failed: %s", typ, validation.Error())
			}
		}
		return nil, nil, fmt.Errorf("invalid %s record: schema validation failed", typ)
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return nil, nil, fmt.Errorf("decoding %s: %w", typ, err)
	}
	if err := semantic(value); err != nil {
		return nil, nil, err
	}
	unknown := unknownFields(obj, reflect.TypeOf(value), "")
	sort.Strings(unknown)
	return value, unknown, nil
}

func semantic(value any) error {
	switch v := value.(type) {
	case *Header:
		if strings.ContainsRune(v.Repository, 0) {
			return fmt.Errorf("repository identifier must not contain NUL")
		}
	case *File:
		if !ValidPath(v.Path, false) {
			return fmt.Errorf("invalid file path %q", v.Path)
		}
	case *Node:
		kind, file, q, err := ParseIdentity(v.Identity)
		if err != nil {
			return err
		}
		if kind != v.Kind || file != v.File || q != v.QualifiedName {
			return fmt.Errorf("identity components disagree with node fields")
		}
		if v.Span != nil && !validSpan(*v.Span) {
			return fmt.Errorf("invalid node span")
		}
		if v.File != "." && v.Span == nil {
			return fmt.Errorf("source node requires span")
		}
		if v.Kind == "external" && (v.File != "." || v.Span != nil) {
			return fmt.Errorf("external nodes must be synthetic")
		}
		if v.File == "." {
			if v.Span != nil {
				return fmt.Errorf("synthetic nodes must omit span")
			}
			hash, err := SyntheticHash(v.Attributes)
			if err != nil {
				return err
			}
			if hash != v.ContentHash {
				return fmt.Errorf("synthetic node content hash does not match attributes")
			}
		}
	case *Edge:
		if _, _, _, err := ParseIdentity(v.Source); err != nil {
			return err
		}
		if _, _, _, err := ParseIdentity(v.Target); err != nil {
			return err
		}
		for _, e := range v.Evidence {
			if !ValidPath(e.File, false) || !validSpan(e.Span) {
				return fmt.Errorf("invalid edge evidence")
			}
		}
	case *Diagnostic:
		if v.File != "" && !ValidPath(v.File, false) {
			return fmt.Errorf("invalid diagnostic file")
		}
	}
	return nil
}

func validSpan(s Span) bool {
	return s.StartLine >= 1 && s.EndLine >= s.StartLine && s.StartColumn >= 0 && s.EndColumn >= 0 && (s.EndLine != s.StartLine || s.EndColumn >= s.StartColumn)
}

func unknownFields(value any, typ reflect.Type, prefix string) []string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	var result []string
	if typ.Kind() == reflect.Slice {
		if arr, ok := value.([]any); ok {
			for i, v := range arr {
				result = append(result, unknownFields(v, typ.Elem(), fmt.Sprintf("%s[%d]", prefix, i))...)
			}
		}
		return result
	}
	if typ.Kind() != reflect.Struct {
		return result
	} // Attribute maps are deliberately open.
	obj, ok := value.(map[string]any)
	if !ok {
		return result
	}
	fields := map[string]reflect.Type{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		fields[strings.Split(f.Tag.Get("json"), ",")[0]] = f.Type
	}
	for key, v := range obj {
		p := key
		if prefix != "" {
			p = prefix + "." + key
		}
		if t, ok := fields[key]; ok {
			result = append(result, unknownFields(v, t, p)...)
		} else {
			result = append(result, p)
		}
	}
	return result
}
