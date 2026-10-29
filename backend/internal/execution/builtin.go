package execution

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// LoadBuiltinHarness captures content and a reviewed capability, never host launch paths.
// The unused launch arguments retain source compatibility with startup callers.
func LoadBuiltinHarness(directory, _, _ string) (HarnessSnapshot, error) {
	prompt, err := os.ReadFile(filepath.Join(directory, "system-prompt.md"))
	if err != nil {
		return HarnessSnapshot{}, errors.New("read builtin Harness system prompt failed")
	}
	h := HarnessSnapshot{
		ID: BuiltinHarnessID, Name: "Browser Harness", Revision: 1,
		SystemPrompt: string(prompt), Skills: []Skill{},
		MCP: map[string]MCPServer{"browser-harness": {
			Capability: BuiltinHarnessID, Config: map[string]any{}, SecretRefs: map[string]string{},
		}},
	}
	skillRoot := filepath.Join(directory, "skills")
	entries, err := os.ReadDir(skillRoot)
	if err != nil {
		return HarnessSnapshot{}, errors.New("read builtin Harness skills failed")
	}
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink != 0 {
			return HarnessSnapshot{}, errors.New("builtin Harness resources must not be symlinks")
		}
		if !entry.IsDir() {
			continue
		}
		root := filepath.Join(skillRoot, entry.Name())
		skill := Skill{Name: entry.Name()}
		found := false
		err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&fs.ModeSymlink != 0 || (!entry.IsDir() && !entry.Type().IsRegular()) {
				return errors.New("builtin Harness resources must be regular files")
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(root, filePath)
			if err != nil {
				return err
			}
			content, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			if relative == "SKILL.md" {
				skill.Content, found = string(content), true
			} else {
				skill.Files = append(skill.Files, SkillFile{Path: filepath.ToSlash(relative), Content: string(content)})
			}
			return nil
		})
		if err != nil || !found {
			return HarnessSnapshot{}, errors.New("read builtin Harness skill resources failed")
		}
		h.Skills = append(h.Skills, skill)
	}
	return h, h.Validate()
}
