package canonical

import (
	"errors"
	"strconv"
	"strings"
)

func number(value string) ([]byte, error) {
	if len(value) > 1024 {
		return nil, errors.New("canonical number token exceeds limit")
	}
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "-")
	exponent := 0
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		var err error
		exponent, err = strconv.Atoi(value[index+1:])
		if err != nil || exponent < -10000 || exponent > 10000 {
			return nil, errors.New("canonical number exponent exceeds limit")
		}
		value = value[:index]
	}
	if index := strings.IndexByte(value, '.'); index >= 0 {
		exponent -= len(value) - index - 1
		value = value[:index] + value[index+1:]
	}
	if len(value) > 256 {
		return nil, errors.New("canonical number mantissa exceeds limit")
	}
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return []byte("0"), nil
	}
	trimmed := strings.TrimRight(value, "0")
	exponent += len(value) - len(trimmed)
	if exponent < -10000 || exponent > 10000 {
		return nil, errors.New("canonical normalized exponent exceeds limit")
	}
	if negative {
		trimmed = "-" + trimmed
	}
	return []byte(trimmed + "e" + strconv.Itoa(exponent)), nil
}
