package kernel

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// validateJSONObject rejects duplicate keys at every level before decoding maps.
func validateJSONObject(data []byte) error {
	if len(data) > 4<<20 {
		return errors.New("JSON object exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return errors.New("JSON root must be an object")
	}
	if err := consumeContainer(decoder, '{', 1); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

func consumeContainer(decoder *json.Decoder, kind json.Delim, depth int) error {
	if depth > 64 {
		return errors.New("JSON nesting exceeds limit")
	}
	seen := make(map[string]bool)
	for decoder.More() {
		if kind == '{' {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return errors.New("invalid or duplicate JSON key")
			}
			seen[name] = true
		}
		value, err := decoder.Token()
		if err != nil {
			return errors.New("invalid JSON value")
		}
		if delimiter, ok := value.(json.Delim); ok {
			if delimiter != '{' && delimiter != '[' {
				return errors.New("invalid JSON container")
			}
			if err := consumeContainer(decoder, delimiter, depth+1); err != nil {
				return err
			}
		}
	}
	closing, err := decoder.Token()
	if err != nil || (kind == '{' && closing != json.Delim('}')) || (kind == '[' && closing != json.Delim(']')) {
		return errors.New("invalid JSON closing delimiter")
	}
	return nil
}
