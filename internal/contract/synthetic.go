package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// SyntheticHash hashes semantic attributes when no source span exists.
// Restricting numbers to interoperable integers avoids cross-language rounding.
func SyntheticHash(attributes map[string]any) (string, error) {
	raw, err := json.Marshal(attributes)
	if err != nil {
		return "", fmt.Errorf("encoding synthetic attributes: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := canonicalJSON(&out, value); err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(out.Bytes())), nil
}

func canonicalJSON(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(v))
	case string:
		quoteJSON(out, v)
	case json.Number:
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil || n < -9007199254740991 || n > 9007199254740991 {
			return fmt.Errorf("synthetic attributes require safe integers")
		}
		out.WriteString(strconv.FormatInt(n, 10))
	case []any:
		out.WriteByte('[')
		for i, child := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := canonicalJSON(out, child); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			quoteJSON(out, key)
			out.WriteByte(':')
			if err := canonicalJSON(out, v[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported synthetic attribute type %T", v)
	}
	return nil
}

func quoteJSON(out *bytes.Buffer, s string) {
	out.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 32 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}
