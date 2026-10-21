package operation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

type schema struct {
	Type                 string            `json:"type"`
	Required             []string          `json:"required,omitempty"`
	Properties           map[string]schema `json:"properties,omitempty"`
	Items                *schema           `json:"items,omitempty"`
	AdditionalProperties *bool             `json:"additionalProperties,omitempty"`
	MinLength            *int              `json:"minLength,omitempty"`
}

func compileSchema(raw json.RawMessage) (schema, error) {
	var compiled schema
	if err := decodeOneJSON(raw, &compiled, true); err != nil {
		return schema{}, fmt.Errorf("decode input schema: %w", err)
	}
	if compiled.Type == "" {
		return schema{}, fmt.Errorf("input schema type is required")
	}
	if err := compiled.validateDefinition("$"); err != nil {
		return schema{}, err
	}
	return compiled, nil
}

func (s schema) validateDefinition(path string) error {
	switch s.Type {
	case "object", "array", "string", "integer", "number", "boolean", "null":
	default:
		return fmt.Errorf("%s has unsupported schema type %q", path, s.Type)
	}
	if s.MinLength != nil && (s.Type != "string" || *s.MinLength < 0) {
		return fmt.Errorf("%s has invalid minLength", path)
	}
	if s.Items != nil {
		if s.Type != "array" {
			return fmt.Errorf("%s may only declare items for an array", path)
		}
		if err := s.Items.validateDefinition(path + "[]"); err != nil {
			return err
		}
	}
	for name, property := range s.Properties {
		if err := property.validateDefinition(path + "." + name); err != nil {
			return err
		}
	}
	return nil
}

func (s schema) validateRaw(raw json.RawMessage) error {
	var value any
	if err := decodeOneJSON(raw, &value, false); err != nil {
		return fmt.Errorf("arguments must contain one JSON value: %w", err)
	}
	if err := s.validateValue(value, "$"); err != nil {
		return fmt.Errorf("arguments do not match input schema: %w", err)
	}
	return nil
}

func (s schema) validateValue(value any, path string) error {
	switch s.Type {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		for _, name := range s.Required {
			if _, exists := object[name]; !exists {
				return fmt.Errorf("%s.%s is required", path, name)
			}
		}
		for name, item := range object {
			property, exists := s.Properties[name]
			if !exists {
				if s.AdditionalProperties != nil && !*s.AdditionalProperties {
					return fmt.Errorf("%s.%s is not allowed", path, name)
				}
				continue
			}
			if err := property.validateValue(item, path+"."+name); err != nil {
				return err
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if s.Items != nil {
			for index, item := range array {
				if err := s.Items.validateValue(item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		if s.MinLength != nil && len([]rune(text)) < *s.MinLength {
			return fmt.Errorf("%s must contain at least %d characters", path, *s.MinLength)
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be an integer", path)
		}
		parsed, err := number.Float64()
		if err != nil || math.Trunc(parsed) != parsed {
			return fmt.Errorf("%s must be an integer", path)
		}
	case "number":
		if _, ok := value.(json.Number); !ok {
			return fmt.Errorf("%s must be a number", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "null":
		if value != nil {
			return fmt.Errorf("%s must be null", path)
		}
	}
	return nil
}

func decodeOneJSON(raw []byte, target any, disallowUnknown bool) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if disallowUnknown {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}
