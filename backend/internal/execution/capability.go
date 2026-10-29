package execution

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

var contentDigest = regexp.MustCompile(`^(sha256:)?[a-f0-9]{64}$`)
var secretReference = regexp.MustCompile(`^secret://([A-Za-z0-9][A-Za-z0-9._-]*)/([A-Za-z0-9][A-Za-z0-9._-]*)$`)

func validateMetadata(projectID, versionID, digest string) error {
	for _, id := range []string{projectID, versionID} {
		if id != "" && (!resourceName.MatchString(id) || reservedName(id)) {
			return errors.New("invalid snapshot identity metadata")
		}
	}
	if digest != "" && !contentDigest.MatchString(digest) {
		return errors.New("invalid snapshot content digest")
	}
	return nil
}

// Validate checks wire safety. Deployment availability and config/secret whitelists
// are enforced by the trusted host capability resolver, not snapshot launch fields.
func (s MCPServer) Validate() error {
	if !resourceName.MatchString(s.Capability) || reservedName(s.Capability) || s.Config == nil || s.SecretRefs == nil {
		return errors.New("MCP capability, config and secret_refs are required")
	}
	if err := validateMetadata("", s.VersionID, s.ContentDigest); err != nil {
		return err
	}
	raw, err := json.Marshal(s.Config)
	if err != nil {
		return errors.New("MCP config must contain JSON values")
	}
	var config any
	if json.Unmarshal(raw, &config) != nil || !safeConfig(config, 0) {
		return errors.New("MCP config must contain non-sensitive JSON values")
	}
	for name, ref := range s.SecretRefs {
		parts := secretReference.FindStringSubmatch(ref)
		if !resourceName.MatchString(name) || reservedName(name) || len(parts) != 3 ||
			reservedName(parts[1]) || reservedName(parts[2]) {
			return errors.New("invalid scoped MCP secret reference")
		}
	}
	if s.Capability == BuiltinHarnessID && (len(s.Config) != 0 || len(s.SecretRefs) != 0) {
		return errors.New("browser capability does not accept public config or secrets")
	}
	return nil
}

func safeConfig(value any, depth int) bool {
	if depth > 64 {
		return false
	}
	switch value := value.(type) {
	case nil, bool, float64:
		return true
	case string:
		return !strings.ContainsRune(value, 0) && !strings.Contains(strings.ToLower(value), "env:")
	case []any:
		for _, item := range value {
			if !safeConfig(item, depth+1) {
				return false
			}
		}
		return true
	case map[string]any:
		for name, item := range value {
			if !resourceName.MatchString(name) || reservedName(name) || credentialName.MatchString(name) ||
				strings.EqualFold(name, "command") || strings.EqualFold(name, "args") ||
				strings.EqualFold(name, "env") || strings.EqualFold(name, "host_only_tools") || !safeConfig(item, depth+1) {
				return false
			}
		}
		return true
	}
	return false
}

func decodeSnapshot(raw []byte, target any, required ...string) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return errors.New("invalid snapshot shape")
	}
	for _, name := range required {
		value, present := fields[name]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("missing snapshot fields")
		}
	}
	for _, name := range []string{"project_id", "version_id", "content_digest"} {
		if value, present := fields[name]; present {
			var text string
			if json.Unmarshal(value, &text) != nil || text == "" {
				return errors.New("invalid snapshot identity metadata")
			}
		}
	}
	if value, present := fields["runtime_ids"]; present {
		var ids []string
		if json.Unmarshal(value, &ids) != nil || len(ids) == 0 {
			return errors.New("invalid Harness runtime_ids")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return errors.New("invalid snapshot fields")
	}
	return nil
}

func (s *MCPServer) UnmarshalJSON(raw []byte) error {
	type wire MCPServer
	var decoded wire
	if err := decodeSnapshot(raw, &decoded, "capability", "config", "secret_refs"); err != nil {
		return err
	}
	*s = MCPServer(decoded)
	return nil
}

func (h *HarnessSnapshot) UnmarshalJSON(raw []byte) error {
	type wire HarnessSnapshot
	var decoded wire
	if err := decodeSnapshot(raw, &decoded); err != nil {
		return err
	}
	*h = HarnessSnapshot(decoded)
	return nil
}

func (s *Skill) UnmarshalJSON(raw []byte) error {
	type wire Skill
	var decoded wire
	if err := decodeSnapshot(raw, &decoded); err != nil {
		return err
	}
	*s = Skill(decoded)
	return nil
}
