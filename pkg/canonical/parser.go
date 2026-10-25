package canonical

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// jsonParser only receives a complete json.Valid input with valid UTF-8 and
// paired Unicode escapes. Duplicate decoded keys and AKOS limits are checked
// during traversal; numbers still use the exact decimal canonicalizer.
type jsonParser struct {
	data []byte
	pos  int
}

func (p *jsonParser) space() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonParser) stringSpan() ([]byte, bool) {
	start, escaped := p.pos, false
	p.pos++
	for {
		switch p.data[p.pos] {
		case '\\':
			escaped = true
			p.pos += 2
		case '"':
			p.pos++
			return p.data[start:p.pos], escaped
		default:
			p.pos++
		}
	}
}

func (p *jsonParser) value(dst []byte, depth int) ([]byte, error) {
	p.space()
	switch p.data[p.pos] {
	case '{', '[':
		if depth >= 64 {
			return nil, errors.New("canonical JSON nesting exceeds limit")
		}
		if p.data[p.pos] == '{' {
			return p.object(dst, depth+1)
		}
		p.pos++
		p.space()
		dst = append(dst, '[')
		if p.data[p.pos] != ']' {
			for {
				var err error
				dst, err = p.value(dst, depth+1)
				if err != nil {
					return nil, err
				}
				p.space()
				if p.data[p.pos] == ']' {
					break
				}
				p.pos++ // Comma; syntax was checked before traversal.
				dst = append(dst, ',')
			}
		}
		p.pos++
		return append(dst, ']'), nil
	case '"':
		raw, escaped := p.stringSpan()
		if !escaped || minimalString(raw) {
			return append(dst, raw...), nil
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return nil, errors.New("invalid canonical JSON string")
		}
		return append(dst, quote(value)...), nil
	default:
		start := p.pos
		for p.pos < len(p.data) && !strings.ContainsRune(" \t\n\r,]}", rune(p.data[p.pos])) {
			p.pos++
		}
		raw := p.data[start:p.pos]
		if raw[0] == 't' || raw[0] == 'f' || raw[0] == 'n' {
			return append(dst, raw...), nil
		}
		encoded, err := number(string(raw))
		if err != nil {
			return nil, err
		}
		return append(dst, encoded...), nil
	}
}

type jsonField struct {
	key     []byte
	encoded []byte
	start   int
}

// The short JSON escapes already are AKOS's minimal representation. Unicode
// and escaped slash forms need decoding; skip each escaped backslash pair so
// a literal "\\u" is not mistaken for a Unicode escape.
func minimalString(raw []byte) bool {
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] == '\\' {
			i++
			if raw[i] == 'u' || raw[i] == '/' {
				return false
			}
		}
	}
	return true
}

func (p *jsonParser) object(dst []byte, depth int) ([]byte, error) {
	p.pos++
	p.space()
	var small [16]jsonField
	fields := small[:0]
	if p.data[p.pos] != '}' {
		for {
			raw, escaped := p.stringSpan()
			key := raw[1 : len(raw)-1]
			if escaped {
				var decoded string
				if json.Unmarshal(raw, &decoded) != nil {
					return nil, errors.New("invalid canonical JSON key")
				}
				key = []byte(decoded)
				if !minimalString(raw) {
					raw = []byte(quote(decoded))
				}
			}
			p.space()
			p.pos++ // Colon.
			p.space()
			start := p.pos
			p.skipValue()
			fields = append(fields, jsonField{key: key, encoded: raw, start: start})
			p.space()
			if p.data[p.pos] == '}' {
				break
			}
			p.pos++ // Comma.
			p.space()
		}
	}
	p.pos++
	end := p.pos
	// UTF-8 byte order equals Unicode scalar order for already validated keys.
	slices.SortFunc(fields, func(a, b jsonField) int { return bytes.Compare(a.key, b.key) })
	dst = append(dst, '{')
	for i, field := range fields {
		if i > 0 {
			if bytes.Equal(fields[i-1].key, field.key) {
				return nil, errors.New("duplicate canonical JSON key")
			}
			dst = append(dst, ',')
		}
		dst = append(dst, field.encoded...)
		dst = append(dst, ':')
		p.pos = field.start
		var err error
		dst, err = p.value(dst, depth)
		if err != nil {
			return nil, err
		}
	}
	p.pos = end
	return append(dst, '}'), nil
}

// Find a value's original span in an already syntax-validated input. Encoding
// happens only after key sorting, directly into the single output buffer.
// This scan is iterative; the encoding traversal still enforces depth <= 64.
func (p *jsonParser) skipValue() {
	switch p.data[p.pos] {
	case '"':
		p.stringSpan()
	case '{', '[':
		nesting := 0
		for {
			switch p.data[p.pos] {
			case '"':
				p.stringSpan()
				continue
			case '{', '[':
				nesting++
			case '}', ']':
				nesting--
			}
			p.pos++
			if nesting == 0 {
				return
			}
		}
	default:
		for p.pos < len(p.data) && !strings.ContainsRune(" \t\n\r,]}", rune(p.data[p.pos])) {
			p.pos++
		}
	}
}
