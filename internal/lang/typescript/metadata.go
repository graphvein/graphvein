package typescript

import "encoding/json"

// DecodeMetadata exposes the existing JSONC decoder to link-time metadata readers.
func DecodeMetadata(source []byte, value any) error {
	b, err := jsonc(source)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, value)
}
