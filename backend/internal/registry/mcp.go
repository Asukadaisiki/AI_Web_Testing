package registry

import (
	"errors"
	"fmt"
	"strings"
)

func (s Schema) Validate() error {
	for name, field := range s.Properties {
		if !slug.MatchString(name) {
			return errors.New("invalid schema property")
		}
		switch field.Type {
		case "string", "boolean", "number", "integer":
		default:
			return errors.New("unsupported schema field type")
		}
		if len(field.Enum) > 0 && field.Type != "string" {
			return errors.New("enum requires string")
		}
	}
	for _, name := range s.Required {
		if _, ok := s.Properties[name]; !ok {
			return errors.New("required schema property is undeclared")
		}
	}
	return nil
}

func ValidateMCP(v MCPVersion) error {
	if !slug.MatchString(v.Name) || v.Capability != "browser-harness" || v.Transport != "stdio" {
		return errors.New("MCP must reference a deployed reviewed capability using stdio")
	}
	// Browser endpoint, artifact namespace, goal and worker identity are host-owned.
	if len(v.ConfigSchema.Properties) != 0 || len(v.SecretSchema.Properties) != 0 {
		return errors.New("browser capability does not accept user configuration or secrets")
	}
	if err := v.ConfigSchema.Validate(); err != nil {
		return err
	}
	return v.SecretSchema.Validate()
}

func ValidateBinding(projectID string, v MCPVersion, config map[string]any, refs map[string]string) error {
	if v.RevokedAt != nil {
		return errors.New("MCP version is revoked")
	}
	if err := v.ConfigSchema.Validate(); err != nil {
		return err
	}
	if err := v.SecretSchema.Validate(); err != nil {
		return err
	}
	for _, key := range v.ConfigSchema.Required {
		if _, ok := config[key]; !ok {
			return fmt.Errorf("missing MCP configuration %s", key)
		}
	}
	for key, value := range config {
		field, ok := v.ConfigSchema.Properties[key]
		if !ok {
			return fmt.Errorf("undeclared MCP configuration %s", key)
		}
		valid := false
		switch field.Type {
		case "string":
			if text, ok := value.(string); ok && !strings.ContainsRune(text, 0) {
				valid = len(field.Enum) == 0
				for _, candidate := range field.Enum {
					valid = valid || text == candidate
				}
			}
		case "boolean":
			_, valid = value.(bool)
		case "number":
			_, valid = value.(float64)
		case "integer":
			if number, ok := value.(float64); ok {
				valid = number == float64(int64(number))
			}
		}
		if !valid {
			return fmt.Errorf("invalid MCP configuration %s", key)
		}
	}
	for _, key := range v.SecretSchema.Required {
		if refs[key] == "" {
			return fmt.Errorf("missing MCP secret reference %s", key)
		}
	}
	prefix := "secret://projects/" + projectID + "/"
	for key, ref := range refs {
		if _, ok := v.SecretSchema.Properties[key]; !ok || !strings.HasPrefix(ref, prefix) || !slug.MatchString(strings.TrimPrefix(ref, prefix)) {
			return errors.New("MCP secrets require declared project-scoped references")
		}
	}
	return nil
}
