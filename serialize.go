package simforge

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

// serializeValue converts a Go value into a JSON-safe representation.
// Handles primitives, maps, slices, structs, time.Time, and common interfaces.
func serializeValue(v any) any {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case string:
		return val
	case bool:
		return val
	case int:
		return val
	case int8:
		return int(val)
	case int16:
		return int(val)
	case int32:
		return int(val)
	case int64:
		return val
	case uint:
		return val
	case uint8:
		return uint(val)
	case uint16:
		return uint(val)
	case uint32:
		return uint(val)
	case uint64:
		return val
	case float32:
		return float64(val)
	case float64:
		return val
	case time.Time:
		return val.UTC().Format("2006-01-02T15:04:05.000Z")
	case json.Marshaler:
		// Types that know how to marshal themselves (e.g., json.RawMessage)
		data, err := val.MarshalJSON()
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		var parsed any
		if json.Unmarshal(data, &parsed) == nil {
			return parsed
		}
		return string(data)
	case error:
		return val.Error()
	case fmt.Stringer:
		return val.String()
	}

	// Use reflection for maps, slices, structs
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		return serializeMap(rv)
	case reflect.Slice, reflect.Array:
		return serializeSlice(rv)
	case reflect.Struct:
		return serializeStruct(rv)
	case reflect.Ptr:
		if rv.IsNil() {
			return nil
		}
		return serializeValue(rv.Elem().Interface())
	default:
		return fmt.Sprintf("%v", v)
	}
}

func serializeMap(rv reflect.Value) map[string]any {
	result := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		key := fmt.Sprintf("%v", iter.Key().Interface())
		result[key] = serializeValue(iter.Value().Interface())
	}
	return result
}

func serializeSlice(rv reflect.Value) []any {
	result := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		result[i] = serializeValue(rv.Index(i).Interface())
	}
	return result
}

func serializeStruct(rv reflect.Value) map[string]any {
	rt := rv.Type()
	result := make(map[string]any)
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}

		// Use json tag name if available
		name := field.Name
		if tag := field.Tag.Get("json"); tag != "" {
			parts := splitTag(tag)
			if parts[0] == "-" {
				continue
			}
			if parts[0] != "" {
				name = parts[0]
			}
		}

		result[name] = serializeValue(rv.Field(i).Interface())
	}
	return result
}

// splitTag splits a struct tag value on commas, returning the parts.
func splitTag(tag string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(tag); i++ {
		if i == len(tag) || tag[i] == ',' {
			parts = append(parts, tag[start:i])
			start = i + 1
		}
	}
	return parts
}

// serializeInputs converts function arguments into a JSON-safe list.
func serializeInputs(args []any) []any {
	result := make([]any, len(args))
	for i, arg := range args {
		result[i] = serializeValue(arg)
	}
	return result
}
