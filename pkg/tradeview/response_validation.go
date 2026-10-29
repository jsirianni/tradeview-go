package tradeview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// responseJSONType keeps reflection details out of generated declarations.
type responseJSONType = reflect.Type

func responseJSONTypeOf[T any]() responseJSONType { return reflect.TypeFor[T]() }

// validateFiniteResponseJSON validates the types hidden behind generated opaque
// unions before a typed operation reports success. This is JSON type validation,
// not business validation: unknown fields/enums and omitted model fields remain
// supported. Model null handling is unchanged; the response itself and its
// required status discriminator must be non-null.
func validateFiniteResponseJSON[T any](data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if object == nil {
		return errors.New("expected a response object")
	}
	typ := responseJSONTypeOf[T]()
	models := responseJSONAlternatives(typ)
	if len(responseJSONFields(models, "s")) != 0 {
		var envelope struct {
			S json.RawMessage `json:"s"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return err
		}
		if len(envelope.S) == 0 || bytes.Equal(envelope.S, []byte("null")) {
			return errors.New("missing or null response status")
		}
		var status string
		if err := json.Unmarshal(envelope.S, &status); err != nil {
			return responseJSONFieldError(err, "s")
		}
		if status != "ok" {
			return errors.New("expected response status ok or a valid broker error")
		}
	}
	return validateResponseJSON(data, typ, "")
}

func responseJSONAlternatives(typ responseJSONType) []responseJSONType {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if variants, ok := _responseJSONUnions[typ]; ok {
		var result []responseJSONType
		for _, variant := range variants {
			result = append(result, responseJSONAlternatives(variant)...)
		}
		return result
	}
	return []responseJSONType{typ}
}

func validateResponseJSON(data []byte, typ responseJSONType, path string) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil // Preserve encoding/json's existing model-field null behavior.
	}
	if variants, ok := _responseJSONUnions[typ]; ok {
		if len(variants) == 1 {
			return validateResponseJSON(data, variants[0], path)
		}
		// Object alternatives can overlap (e.g. SuccessResponse and the richer
		// transaction response). Validate every supplied field known to any
		// alternative so a generic object cannot conceal a malformed payload.
		if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
			var objects []responseJSONType
			for _, variant := range responseJSONAlternatives(typ) {
				if variant.Kind() == reflect.Struct {
					objects = append(objects, variant)
				}
			}
			if len(objects) > 0 {
				return validateResponseJSONObject(data, objects, path)
			}
		}
		var first error
		for _, variant := range variants {
			if err := validateResponseJSON(data, variant, path); err == nil {
				return nil
			} else if first == nil {
				first = err
			}
		}
		return first
	}
	// These compatibility models preflight scalar expansion and symbol counts
	// before allocating typed arrays. Their custom decoder checks field types.
	if typ == responseJSONTypeOf[SymbolInfoResponse]() || typ == responseJSONTypeOf[SymbolInfoArrays]() {
		return json.Unmarshal(data, reflect.New(typ).Interface())
	}
	switch typ.Kind() {
	case reflect.Struct:
		return validateResponseJSONObject(data, []responseJSONType{typ}, path)
	case reflect.Slice, reflect.Array:
		return validateResponseJSONCollection(data, typ, path, false)
	case reflect.Map:
		return validateResponseJSONCollection(data, typ, path, true)
	default:
		return responseJSONFieldError(json.Unmarshal(data, reflect.New(typ).Interface()), path)
	}
}

// Decode one element at a time rather than allocating a RawMessage slice for
// every untrusted array. Preserve duplicate map entries for type checking too.
func validateResponseJSONCollection(data []byte, typ responseJSONType, path string, object bool) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	want := json.Delim('[')
	if object {
		want = json.Delim('{')
	}
	if token != want {
		return responseJSONFieldError(json.Unmarshal(data, reflect.New(typ).Interface()), path)
	}
	for index := 0; decoder.More(); index++ {
		itemPath := path
		if object {
			if _, err := decoder.Token(); err != nil {
				return err
			}
		} else {
			itemPath = responseJSONPath(path, fmt.Sprint(index))
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if err := validateResponseJSON(value, typ.Elem(), itemPath); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func validateResponseJSONObject(data []byte, models []responseJSONType, path string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		// Let encoding/json supply an inspectable type error.
		return responseJSONFieldError(json.Unmarshal(data, reflect.New(models[0]).Interface()), path)
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := token.(string)
		if !ok {
			return errors.New("invalid response object property")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		fields := responseJSONFields(models, name)
		var first error
		for _, field := range fields {
			if err := validateResponseJSON(value, field.Type, responseJSONPath(path, field.Name)); err == nil {
				first = nil
				break
			} else if first == nil {
				first = err
			}
		}
		if first != nil {
			return first
		}
	}
	_, err = decoder.Token()
	return err
}

type responseJSONField struct {
	Name string
	Type responseJSONType
}

func responseJSONFields(models []responseJSONType, name string) []responseJSONField {
	var fields []responseJSONField
	for _, model := range models {
		if model.Kind() != reflect.Struct {
			continue
		}
		var folded *responseJSONField
		for field := range model.Fields() {
			if !field.IsExported() {
				continue
			}
			key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if key == "-" {
				continue
			}
			if key == "" {
				key = field.Name
			}
			candidate := responseJSONField{Name: key, Type: field.Type}
			if key == name {
				folded = &candidate
				break
			}
			if folded == nil && strings.EqualFold(key, name) {
				folded = &candidate
			}
		}
		if folded != nil {
			fields = append(fields, *folded)
		}
	}
	return fields
}

func responseJSONPath(parent, field string) string {
	if parent == "" {
		return field
	}
	return parent + "." + field
}

func responseJSONFieldError(err error, path string) error {
	if typeErr, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		typeErr.Field = path
	}
	return err
}
