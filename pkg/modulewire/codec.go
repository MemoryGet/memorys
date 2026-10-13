package modulewire

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemoryGet/memorys/pkg/canonical"
	"github.com/MemoryGet/memorys/pkg/contracts"
)

func invalid() error { return errors.New("invalid remote module DTO") }

var utcTimestamp = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{0,8}[1-9])?Z$`)

func encode(value any) ([]byte, error) {
	object, err := toWire(reflect.ValueOf(value))
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(object)
	if err != nil {
		return nil, invalid()
	}
	if _, err := canonical.JSON(data); err != nil {
		return nil, invalid()
	}
	return data, nil
}

func decode(data []byte, value any) error {
	if _, err := canonical.JSON(data); err != nil {
		return invalid()
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var object any
	if decoder.Decode(&object) != nil {
		return invalid()
	}
	return fromWire(object, reflect.ValueOf(value).Elem())
}

func toWire(value reflect.Value) (any, error) {
	if !value.IsValid() {
		return nil, invalid()
	}
	if value.Kind() == reflect.Pointer {
		return toWire(value.Elem())
	}
	if value.Type() == reflect.TypeOf(time.Time{}) {
		timestamp := value.Interface().(time.Time)
		if timestamp.IsZero() || timestamp.Year() < 1 || timestamp.Year() > 9999 {
			return nil, invalid()
		}
		return timestamp.UTC().Format(time.RFC3339Nano), nil
	}
	switch value.Kind() {
	case reflect.Map:
		if value.Type() != reflect.TypeOf(map[string]float64{}) {
			return nil, invalid()
		}
		object := make(map[string]any, value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			key := iterator.Key().String()
			if !utf8.ValidString(key) {
				return nil, invalid()
			}
			item, err := toWire(iterator.Value())
			if err != nil {
				return nil, err
			}
			object[key] = item
		}
		return object, nil
	case reflect.Struct:
		return encodeStruct(value)
	case reflect.Slice:
		items := make([]any, value.Len())
		for i := range items {
			item, err := toWire(value.Index(i))
			if err != nil {
				return nil, err
			}
			items[i] = item
		}
		return items, nil
	case reflect.String:
		if !utf8.ValidString(value.String()) {
			return nil, invalid()
		}
		return value.String(), nil
	case reflect.Bool:
		return value.Bool(), nil
	case reflect.Int, reflect.Int64:
		return value.Int(), nil
	case reflect.Uint64:
		return value.Uint(), nil
	case reflect.Float32, reflect.Float64:
		if math.IsNaN(value.Float()) || math.IsInf(value.Float(), 0) {
			return nil, invalid()
		}
		if value.Kind() == reflect.Float32 {
			return float32(value.Float()), nil
		}
		return value.Float(), nil
	}
	return nil, invalid()
}

func encodeStruct(value reflect.Value) (map[string]any, error) {
	fields, found := mappings[value.Type()]
	if !found || len(fields) != value.NumField() {
		return nil, invalid()
	}
	object := make(map[string]any)
	for _, field := range fields {
		member := value.FieldByName(field.goName)
		if field.optional && member.IsZero() {
			continue
		}
		encoded, err := toWire(member)
		if err != nil {
			return nil, err
		}
		object[field.wireName] = encoded
	}
	return flatten(value.Type(), object)
}

func fromWire(object any, value reflect.Value) error {
	if object == nil {
		return invalid()
	}
	if value.Kind() == reflect.Pointer {
		value.Set(reflect.New(value.Type().Elem()))
		return fromWire(object, value.Elem())
	}
	if value.Type() == reflect.TypeOf(time.Time{}) {
		text, ok := object.(string)
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if !ok || err != nil || parsed.Year() < 1 || !utcTimestamp.MatchString(text) {
			return invalid()
		}
		value.Set(reflect.ValueOf(parsed.UTC()))
		return nil
	}
	switch value.Kind() {
	case reflect.Map:
		if value.Type() != reflect.TypeOf(map[string]float64{}) {
			return invalid()
		}
		object, ok := object.(map[string]any)
		if !ok {
			return invalid()
		}
		value.Set(reflect.MakeMapWithSize(value.Type(), len(object)))
		for key, item := range object {
			member := reflect.New(value.Type().Elem()).Elem()
			if err := fromWire(item, member); err != nil {
				return err
			}
			value.SetMapIndex(reflect.ValueOf(key), member)
		}
		return nil
	case reflect.Struct:
		return decodeStruct(object, value)
	case reflect.Slice:
		return decodeArray(object, value)
	default:
		return decodeScalar(object, value)
	}
}

func decodeArray(object any, value reflect.Value) error {
	items, ok := object.([]any)
	if !ok {
		return invalid()
	}
	value.Set(reflect.MakeSlice(value.Type(), len(items), len(items)))
	for i, item := range items {
		if err := fromWire(item, value.Index(i)); err != nil {
			return err
		}
	}
	return nil
}

func decodeScalar(object any, value reflect.Value) error {
	switch value.Kind() {
	case reflect.String:
		text, ok := object.(string)
		if !ok {
			return invalid()
		}
		value.SetString(text)
	case reflect.Bool:
		boolean, ok := object.(bool)
		if !ok {
			return invalid()
		}
		value.SetBool(boolean)
	case reflect.Int, reflect.Int64, reflect.Uint64:
		return integer(object, value)
	case reflect.Float32, reflect.Float64:
		return floating(object, value)
	default:
		return invalid()
	}
	return nil
}

func floating(object any, value reflect.Value) error {
	number, ok := object.(json.Number)
	if !ok {
		return invalid()
	}
	decoded, err := number.Float64()
	if err != nil || math.IsNaN(decoded) || math.IsInf(decoded, 0) {
		return invalid()
	}
	original, err := canonical.JSON([]byte(number))
	if err != nil || (decoded == 0 && string(original) != "0") {
		return invalid()
	}
	var scalar any = decoded
	if value.Kind() == reflect.Float32 {
		before := decoded
		decoded = float64(float32(decoded))
		scalar = float32(decoded)
		if math.IsInf(decoded, 0) || (before != 0 && decoded == 0) {
			return invalid()
		}
		value.SetFloat(decoded)
		return nil // Derived vectors use declared IEEE-754 float32 conversion.
	}
	roundtrip, marshalErr := json.Marshal(scalar)
	encoded, canonicalErr := canonical.JSON(roundtrip)
	if err != nil || marshalErr != nil || canonicalErr != nil || !bytes.Equal(original, encoded) {
		return invalid()
	}
	value.SetFloat(decoded)
	return nil
}

func decodeStruct(object any, value reflect.Value) error {
	fields, found := mappings[value.Type()]
	if !found || len(fields) != value.NumField() {
		return invalid()
	}
	members, ok := object.(map[string]any)
	if !ok {
		return invalid()
	}
	members, err := expand(value.Type(), members)
	if err != nil {
		return err
	}
	known := make(map[string]bool)
	for _, field := range fields {
		known[field.wireName] = true
		member, present := members[field.wireName]
		if !present {
			if field.optional {
				continue
			}
			return invalid()
		}
		if err := fromWire(member, value.FieldByName(field.goName)); err != nil {
			return err
		}
	}
	for name := range members {
		if !known[name] {
			return invalid()
		}
	}
	return nil
}

func integer(object any, target reflect.Value) error {
	number, ok := object.(json.Number)
	if !ok {
		return invalid()
	}
	encoded, err := canonical.JSON([]byte(number))
	if err != nil {
		return invalid()
	}
	text := string(encoded)
	if text != "0" {
		parts := strings.Split(text, "e")
		if len(parts) != 2 {
			return invalid()
		}
		exponent, err := strconv.Atoi(parts[1])
		if err != nil || exponent < 0 || len(strings.TrimPrefix(parts[0], "-"))+exponent > 20 {
			return invalid()
		}
		text = parts[0] + strings.Repeat("0", exponent)
	}
	if target.Kind() == reflect.Uint64 {
		value, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return invalid()
		}
		target.SetUint(value)
	} else {
		value, err := strconv.ParseInt(text, 10, target.Type().Bits())
		if err != nil {
			return invalid()
		}
		target.SetInt(value)
	}
	return nil
}

func ensureIDs(ids ...contracts.ID) error {
	for _, id := range ids {
		if id == "" || !utf8.ValidString(string(id)) {
			return invalid()
		}
	}
	return nil
}
