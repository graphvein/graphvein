package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// uniqueKeys avoids silently interpreting an ambiguous duplicate property.
func uniqueKeys(data []byte) error {
	if !json.Valid(data) {
		return fmt.Errorf("invalid JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := walkJSON(decoder); err != nil {
		if _, ok := err.(duplicateKey); ok {
			return fmt.Errorf("invalid JSON object: %w", err)
		}
		return fmt.Errorf("invalid JSON object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("invalid JSON object")
	}
	return nil
}

type duplicateKey string

func (k duplicateKey) Error() string { return fmt.Sprintf("duplicate key %q", string(k)) }

func walkJSON(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delimiter == '{' {
		keys := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("expected object key")
			}
			if keys[name] {
				return duplicateKey(name)
			}
			keys[name] = true
			if err := walkJSON(decoder); err != nil {
				return err
			}
		}
	} else if delimiter == '[' {
		for decoder.More() {
			if err := walkJSON(decoder); err != nil {
				return err
			}
		}
	} else {
		return fmt.Errorf("unexpected delimiter")
	}
	_, err = decoder.Token()
	return err
}
