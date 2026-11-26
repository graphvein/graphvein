package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// setManifest sets one dotted key of the manifest JSON object, creating
// nested objects as needed. raw selects whether value is JSON or a string,
// so the bash driver never has to escape JSON itself.
func setManifest(path, key, value string, raw bool) error {
	doc := map[string]any{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("reading %s: %w", path, err)
	}
	var v any = value
	if raw {
		if err := json.Unmarshal([]byte(value), &v); err != nil {
			return fmt.Errorf("value for %s is not JSON: %w", key, err)
		}
	}
	parts := strings.Split(key, ".")
	cur := doc
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = v
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	if err := checkNoSecret([]byte(b.String())); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func marshalIndent(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	return b.String()
}
