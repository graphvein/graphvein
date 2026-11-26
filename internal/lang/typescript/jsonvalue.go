package typescript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// jsonValue is a decoded JSON value that keeps object keys in source order.
// The compiler walks package.json "exports" and "imports" conditions, and
// tsconfig "paths" patterns, in property order, which a Go map loses.
type jsonValue struct {
	// Kind is one of "null", "bool", "number", "string", "array", "object".
	Kind   string
	Str    string
	Bool   bool
	Items  []jsonValue
	Keys   []string
	Values []jsonValue
}

// get returns an object's property and whether it is present.
func (v jsonValue) get(key string) (jsonValue, bool) {
	if v.Kind != "object" {
		return jsonValue{}, false
	}
	// The last duplicate wins, as in JSON.parse.
	for i := len(v.Keys) - 1; i >= 0; i-- {
		if v.Keys[i] == key {
			return v.Values[i], true
		}
	}
	return jsonValue{}, false
}

// str returns a string property, or "" and false when it is absent or not a string.
func (v jsonValue) str(key string) (string, bool) {
	p, ok := v.get(key)
	if !ok || p.Kind != "string" {
		return "", false
	}
	return p.Str, true
}

// parseJSONValue decodes one JSON document (comments already removed).
func parseJSONValue(data []byte) (jsonValue, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := decodeJSONValue(d)
	if err != nil {
		return jsonValue{}, err
	}
	if _, err := d.Token(); err != io.EOF {
		return jsonValue{}, fmt.Errorf("unexpected trailing JSON")
	}
	return v, nil
}

func decodeJSONValue(d *json.Decoder) (jsonValue, error) {
	token, err := d.Token()
	if err != nil {
		return jsonValue{}, err
	}
	switch t := token.(type) {
	case json.Delim:
		switch t {
		case '{':
			v := jsonValue{Kind: "object"}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return jsonValue{}, err
				}
				key, ok := k.(string)
				if !ok {
					return jsonValue{}, fmt.Errorf("object key is not a string")
				}
				value, err := decodeJSONValue(d)
				if err != nil {
					return jsonValue{}, err
				}
				v.Keys = append(v.Keys, key)
				v.Values = append(v.Values, value)
			}
			if _, err := d.Token(); err != nil {
				return jsonValue{}, err
			}
			return v, nil
		case '[':
			v := jsonValue{Kind: "array", Items: []jsonValue{}}
			for d.More() {
				item, err := decodeJSONValue(d)
				if err != nil {
					return jsonValue{}, err
				}
				v.Items = append(v.Items, item)
			}
			if _, err := d.Token(); err != nil {
				return jsonValue{}, err
			}
			return v, nil
		}
		return jsonValue{}, fmt.Errorf("unexpected %v", t)
	case string:
		return jsonValue{Kind: "string", Str: t}, nil
	case bool:
		return jsonValue{Kind: "bool", Bool: t}, nil
	case json.Number:
		return jsonValue{Kind: "number", Str: t.String()}, nil
	case nil:
		return jsonValue{Kind: "null"}, nil
	}
	return jsonValue{}, fmt.Errorf("unexpected token %v", token)
}
