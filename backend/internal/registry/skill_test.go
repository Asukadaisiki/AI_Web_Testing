package registry

import (
	"archive/zip"
	"bytes"
	"os"
	"testing"
)

func TestSkillCanonicalDigestAndPaths(t *testing.T) {
	a, err := ValidateSkill("login", []File{{Path: "SKILL.md", Content: "hello\r\n"}, {Path: "refs/help.md", Content: "help"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ValidateSkill("login", []File{{Path: "refs/help.md", Content: "help"}, {Path: "SKILL.md", Content: "hello\n"}})
	if err != nil || a.ContentDigest != b.ContentDigest {
		t.Fatalf("canonical digest differs: %v", err)
	}
	for _, name := range []string{"/etc/passwd", "../secret.md", "a/../secret.md", `C:\secret.md`, "SKILL.md/x", "refs/../../secret.md"} {
		_, err := ValidateSkill("login", []File{{Path: "SKILL.md", Content: "x"}, {Path: name, Content: "bad"}})
		if err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if _, err := ValidateSkill("login", []File{{Path: "file.md", Content: "x"}}); err == nil {
		t.Fatal("accepted missing SKILL.md")
	}
	if _, err := ValidateSkill("login", []File{{Path: "SKILL.md", Content: "x"}, {Path: "skill.md", Content: "x"}}); err == nil {
		t.Fatal("accepted case collision")
	}
}

func TestArchiveRejectsSymlinkAndLimits(t *testing.T) {
	for _, mode := range []os.FileMode{os.ModeSymlink | 0777, os.ModeDevice | 0600} {
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		h := &zip.FileHeader{Name: "SKILL.md"}
		h.SetMode(mode)
		f, _ := w.CreateHeader(h)
		_, _ = f.Write([]byte("secret"))
		_ = w.Close()
		if _, err := ParseSkillArchive("login", buf.Bytes()); err == nil {
			t.Fatalf("accepted nonregular mode %v", mode)
		}
	}
	if _, err := ValidateSkill("login", []File{{Path: "SKILL.md", Content: string(make([]byte, MaxSkillBytes+1))}}); err == nil {
		t.Fatal("accepted oversized skill")
	}
}

func TestBindingRejectsUnknownConfigAndHostSecrets(t *testing.T) {
	version := MCPVersion{Capability: "browser-harness", ConfigSchema: Schema{Properties: map[string]Field{"domain": {Type: "string"}}, Required: []string{"domain"}}, SecretSchema: Schema{Properties: map[string]Field{"credential": {Type: "string"}}}}
	if err := ValidateBinding("p1", version, map[string]any{"domain": "example.test"}, map[string]string{"credential": "secret://projects/p1/login"}); err != nil {
		t.Fatal(err)
	}
	for _, input := range []Binding{
		{Config: map[string]any{"command": "bash", "domain": "x"}},
		{Config: map[string]any{"domain": 4}},
		{Config: map[string]any{"domain": "x"}, SecretRefs: map[string]string{"credential": "env:AWS_SECRET"}},
		{Config: map[string]any{"domain": "x"}, SecretRefs: map[string]string{"credential": "secret://projects/p2/login"}},
	} {
		if err := ValidateBinding("p1", version, input.Config, input.SecretRefs); err == nil {
			t.Fatalf("accepted unsafe binding: %#v", input)
		}
	}
}
