package canonical

import (
	"errors"
	"strconv"
)

func hexUnit(data []byte, index int) (uint64, error) {
	if index+4 > len(data) {
		return 0, errors.New("incomplete Unicode escape")
	}
	value, err := strconv.ParseUint(string(data[index:index+4]), 16, 16)
	if err != nil {
		return 0, errors.New("invalid Unicode escape")
	}
	return value, nil
}

// encoding/json replaces unpaired surrogates; reject them before tokenization.
func validateEscapes(data []byte) error {
	quoted := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return errors.New("incomplete JSON escape")
		}
		if data[i] != 'u' {
			continue
		}
		unit, err := hexUnit(data, i+1)
		if err != nil {
			return err
		}
		i += 4
		if unit >= 0xdc00 && unit <= 0xdfff {
			return errors.New("unpaired low surrogate")
		}
		if unit < 0xd800 || unit > 0xdbff {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return errors.New("unpaired high surrogate")
		}
		low, err := hexUnit(data, i+3)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return errors.New("invalid surrogate pair")
		}
		i += 6
	}
	return nil
}
