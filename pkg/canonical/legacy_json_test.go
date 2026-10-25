// Frozen decoder implementation used as a differential oracle for byte and rejection compatibility.
// Keep independent of the production parser so an optimization cannot rewrite its own expectation.
package canonical

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// legacyJSON preserves exact decimal numbers, Unicode and array order. It rejects
// ambiguous keys, invalid Unicode and inputs exceeding the documented limits.
func legacyJSON(data []byte) ([]byte, error) {
	if len(data) > 4<<20 || !utf8.Valid(data) || validateEscapes(data) != nil {
		return nil, errors.New("invalid canonical legacyJSON input")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := legacyReadValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("trailing canonical legacyJSON data")
	}
	if len(value) > 4<<20 {
		return nil, errors.New("canonical legacyJSON output exceeds limit")
	}
	return value, nil
}

func legacyReadValue(decoder *json.Decoder, depth int) ([]byte, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, errors.New("invalid canonical legacyJSON syntax")
	}
	switch value := token.(type) {
	case json.Delim:
		if depth >= 64 {
			return nil, errors.New("canonical legacyJSON nesting exceeds limit")
		}
		if value == '{' {
			return legacyReadObject(decoder, depth+1)
		}
		if value == '[' {
			return legacyReadArray(decoder, depth+1)
		}
	case string:
		return []byte(legacyQuote(value)), nil
	case json.Number:
		return number(string(value))
	case bool:
		if value {
			return []byte("true"), nil
		}
		return []byte("false"), nil
	case nil:
		return []byte("null"), nil
	}
	return nil, errors.New("invalid canonical legacyJSON token")
}

func legacyReadObject(decoder *json.Decoder, depth int) ([]byte, error) {
	fields := make(map[string][]byte)
	var keys []string
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, errors.New("invalid canonical legacyJSON key")
		}
		if _, found := fields[key]; found {
			return nil, errors.New("duplicate canonical legacyJSON key")
		}
		value, err := legacyReadValue(decoder, depth)
		if err != nil {
			return nil, err
		}
		fields[key] = value
		keys = append(keys, key)
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("invalid canonical legacyJSON object")
	}
	sort.Strings(keys) // Valid UTF-8 byte ordering equals Unicode scalar ordering.
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			buffer.WriteByte(',')
		}
		buffer.WriteString(legacyQuote(key))
		buffer.WriteByte(':')
		buffer.Write(fields[key])
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

func legacyReadArray(decoder *json.Decoder, depth int) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('[')
	first := true
	for decoder.More() {
		value, err := legacyReadValue(decoder, depth)
		if err != nil {
			return nil, err
		}
		if !first {
			buffer.WriteByte(',')
		}
		first = false
		buffer.Write(value)
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
		return nil, errors.New("invalid canonical legacyJSON array")
	}
	buffer.WriteByte(']')
	return buffer.Bytes(), nil
}

func legacyQuote(value string) string {
	var buffer strings.Builder
	buffer.WriteByte('"')
	for _, char := range value {
		switch char {
		case '"', '\\':
			buffer.WriteByte('\\')
			buffer.WriteRune(char)
		case '\b':
			buffer.WriteString(`\b`)
		case '\t':
			buffer.WriteString(`\t`)
		case '\n':
			buffer.WriteString(`\n`)
		case '\f':
			buffer.WriteString(`\f`)
		case '\r':
			buffer.WriteString(`\r`)
		default:
			if char < 0x20 {
				const hex = "0123456789abcdef"
				buffer.WriteString(`\u00`)
				buffer.WriteByte(hex[char>>4])
				buffer.WriteByte(hex[char&15])
			} else {
				buffer.WriteRune(char)
			}
		}
	}
	buffer.WriteByte('"')
	return buffer.String()
}
