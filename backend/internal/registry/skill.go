package registry

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
)

const MaxSkillBytes = 4 << 20
const MaxSkillFiles = 128

var slug = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func Digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func ValidateSkill(name string, files []File) (SkillVersion, error) {
	fail := func() (SkillVersion, error) {
		return SkillVersion{}, errors.New("invalid skill package: require root SKILL.md, regular UTF-8 content files and safe unique relative paths")
	}
	if !slug.MatchString(name) || len(files) == 0 || len(files) > MaxSkillFiles {
		return fail()
	}
	normalized := append([]File(nil), files...)
	seen := map[string]bool{}
	total := 0
	found := false
	for i, f := range normalized {
		key := strings.ToLower(f.Path)
		ext := strings.ToLower(path.Ext(f.Path))
		if !execution.SafeSkillPath(f.Path) || seen[key] || !utf8.ValidString(f.Content) || strings.ContainsRune(f.Content, 0) {
			return fail()
		}
		switch ext {
		case ".md", ".txt", ".json", ".yaml", ".yml", ".csv":
		default:
			return fail()
		}
		if f.Path == "SKILL.md" {
			found = strings.TrimSpace(f.Content) != ""
		}
		total += len(f.Content)
		if total > MaxSkillBytes {
			return fail()
		}
		seen[key] = true
		normalized[i].Content = strings.ReplaceAll(strings.ReplaceAll(f.Content, "\r\n", "\n"), "\r", "\n")
	}
	for key := range seen {
		for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				return fail()
			}
		}
	}
	if !found {
		return fail()
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Path < normalized[j].Path })
	return SkillVersion{Name: name, Files: normalized, ContentDigest: Digest(normalized), Status: "draft"}, nil
}

func ParseSkillArchive(name string, archive []byte) (SkillVersion, error) {
	if len(archive) > MaxSkillBytes {
		return SkillVersion{}, errors.New("skill archive exceeds compressed size limit")
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return SkillVersion{}, errors.New("invalid skill ZIP archive")
	}
	if len(reader.File) > MaxSkillFiles {
		return SkillVersion{}, errors.New("too many archive entries")
	}
	files := []File{}
	total := int64(0)
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			if !execution.SafeSkillPath(strings.TrimSuffix(entry.Name, "/")) {
				return SkillVersion{}, errors.New("invalid directory path")
			}
			continue
		}
		if !entry.Mode().IsRegular() || entry.UncompressedSize64 > MaxSkillBytes || !execution.SafeSkillPath(entry.Name) {
			return SkillVersion{}, errors.New("archive contains unsafe file")
		}
		stream, err := entry.Open()
		if err != nil {
			return SkillVersion{}, err
		}
		data, readErr := io.ReadAll(io.LimitReader(stream, int64(MaxSkillBytes)+1-total))
		_ = stream.Close()
		if readErr != nil {
			return SkillVersion{}, readErr
		}
		total += int64(len(data))
		if total > MaxSkillBytes {
			return SkillVersion{}, errors.New("skill archive expansion exceeds limit")
		}
		files = append(files, File{Path: entry.Name, Content: string(data)})
	}
	return ValidateSkill(name, files)
}
