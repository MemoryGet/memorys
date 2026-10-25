// Package canonical implements the platform's AKOS-CJSON/1 byte encoding.
// This is not RFC 8785/JCS. Domain/schema validation precedes content hashing.
package canonical

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// JSON preserves exact decimal numbers, Unicode and array order. It rejects
// ambiguous keys, invalid Unicode and inputs exceeding the documented limits.
func JSON(data []byte) ([]byte, error) {
	if len(data) > 4<<20 || !utf8.Valid(data) || validateEscapes(data) != nil || !json.Valid(data) {
		return nil, errors.New("invalid canonical JSON input")
	}
	// Validate syntax with the standard library, then traverse byte spans without
	// allocating a Decoder.Token value for every artifact/index key and scalar.
	p := jsonParser{data: data}
	value, err := p.value(make([]byte, 0, len(data)), 0)
	if err != nil {
		return nil, err
	}
	if len(value) > 4<<20 {
		return nil, errors.New("canonical JSON output exceeds limit")
	}
	return value, nil
}

func quote(value string) string {
	var buffer strings.Builder
	buffer.Grow(len(value) + 2)
	buffer.WriteByte('"')
	start := 0
	for i := 0; i < len(value); i++ {
		char := value[i]
		if char >= 0x20 && char != '"' && char != '\\' {
			continue
		}
		buffer.WriteString(value[start:i])
		switch char {
		case '"', '\\':
			buffer.WriteByte('\\')
			buffer.WriteByte(char)
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
			const hex = "0123456789abcdef"
			buffer.WriteString(`\u00`)
			buffer.WriteByte(hex[char>>4])
			buffer.WriteByte(hex[char&15])
		}
		start = i + 1
	}
	buffer.WriteString(value[start:])
	buffer.WriteByte('"')
	return buffer.String()
}
