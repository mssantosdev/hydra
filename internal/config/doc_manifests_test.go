package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every manifest shown in the documentation is loaded by the same code that loads a user's, and
// checked against the repository count it declares.
//
// Reading them is not enough. A schema-2 group nests repositories directly, and under schema 3 that
// same text parses as a group with ZERO repositories — valid YAML, no error, the repository silently
// gone. Six documented examples shipped in that state, and eyeballing found four of them.
//
// The file list is a glob rather than a hand-maintained slice: the previous list needed a reactive
// patch each time a document grew an example, and shipped one release without the template a user
// is told to copy verbatim.

// docManifestRoot is the repository root, two levels up from this package.
func docManifestRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func docManifestFiles(t *testing.T) []string {
	t.Helper()
	root := docManifestRoot(t)
	var files []string
	for _, pattern := range []string{
		"*.md", "*.example", "docs/*.md", "docs/*.html", "docs/**/*.md", "skills/hydra/*.md",
	} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no documentation files found; the glob no longer matches the tree")
	}
	return files
}

var (
	fencedYAML = regexp.MustCompile("(?s)```yaml\n(.*?)```")
	htmlPlate  = regexp.MustCompile(`(?s)<div class="plate">(.*?)</div>`)
	htmlTag    = regexp.MustCompile(`<[^>]+>`)
)

// manifestBlocks extracts every candidate manifest from one file.
func manifestBlocks(path, text string) []string {
	switch {
	case strings.HasSuffix(path, ".example"), strings.HasSuffix(path, ".yaml"):
		return []string{text}
	case strings.HasSuffix(path, ".html"):
		var out []string
		for _, m := range htmlPlate.FindAllStringSubmatch(text, -1) {
			// Every plate is considered, not only one starting with `version:`. Anchoring on the
			// first line means a comment above the example removes it from the check silently.
			out = append(out, htmlUnescape(htmlTag.ReplaceAllString(m[1], "")))
		}
		return out
	default:
		var out []string
		for _, m := range fencedYAML.FindAllStringSubmatch(text, -1) {
			out = append(out, m[1])
		}
		return out
	}
}

func htmlUnescape(s string) string {
	for from, to := range map[string]string{"&lt;": "<", "&gt;": ">", "&amp;": "&", "&quot;": `"`, "&#39;": "'"} {
		s = strings.ReplaceAll(s, from, to)
	}
	return s
}

// isSkeleton reports whether a block stands in for SHAPE rather than being a real manifest.
//
// Placeholders are the only exemption, and they are recognised on the parsed document below rather
// than by scanning the text: a substring test for "<" skips any manifest merely containing a URL or
// a comparison in a comment.
func isSkeleton(block string) bool {
	return strings.Contains(block, "<group>") ||
		strings.Contains(block, "<alias>") ||
		strings.Contains(block, "<git-url>") ||
		strings.Contains(block, "<dir>")
}

func TestDocumentedManifestsLoadAndKeepTheirRepositories(t *testing.T) {
	root := docManifestRoot(t)
	checked := 0

	for _, path := range docManifestFiles(t) {
		raw, err := os.ReadFile(path) //nolint:gosec // G304: path comes from a glob over this repo
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		rel, _ := filepath.Rel(root, path)

		for i, block := range manifestBlocks(path, string(raw)) {
			if !strings.Contains(block, "groups:") || isSkeleton(block) {
				continue
			}

			// The declared count comes from the TEXT, so a group that lost its repositories to the
			// schema-2 nesting is a disagreement rather than an agreed zero.
			declared := declaredRepoCount(t, rel, i+1, block)
			if declared < 0 {
				continue
			}

			dir := t.TempDir()
			manifest := filepath.Join(dir, ".hydra", "config.yaml")
			if err := os.MkdirAll(filepath.Dir(manifest), 0o750); err != nil {
				t.Fatalf("%s #%d: %v", rel, i+1, err)
			}
			if err := os.WriteFile(manifest, []byte(block), 0o600); err != nil {
				t.Fatalf("%s #%d: %v", rel, i+1, err)
			}

			loaded, err := Load(manifest)
			if err != nil {
				t.Errorf("%s #%d does not load: %v\n%s", rel, i+1, err, block)
				continue
			}
			if seen := len(loaded.Repos()); seen != declared {
				t.Errorf("%s #%d declares %d repository(ies), hydra sees %d — a group nested the "+
					"schema-2 way parses as a group with none\n%s", rel, i+1, declared, seen, block)
			}
			checked++
		}
	}

	if checked == 0 {
		t.Fatal("no documented manifest was checked; the extraction no longer matches the docs")
	}
	t.Logf("checked %d documented manifest(s)", checked)
}

// declaredRepoCount counts the repositories a block's TEXT declares, or -1 when the block is not a
// manifest. It reads the nesting itself rather than trusting the parse, which is the whole point:
// the parse is what silently agrees with a group that lost its contents.
func declaredRepoCount(t *testing.T, rel string, index int, block string) int {
	t.Helper()
	var doc struct {
		Version string `yaml:"version"`
		Groups  map[string]struct {
			Repos map[string]struct{} `yaml:"repos"`
		} `yaml:"groups"`
	}
	if err := yaml.Unmarshal([]byte(block), &doc); err != nil {
		t.Errorf("%s #%d: invalid YAML: %v", rel, index, err)
		return -1
	}
	if doc.Version == "" {
		return -1
	}
	total := 0
	for name, group := range doc.Groups {
		if group.Repos == nil {
			t.Errorf("%s #%d: group %q has no `repos:` key, so hydra reads it as a group with no "+
				"repositories (schema-2 nesting)", rel, index, name)
			continue
		}
		total += len(group.Repos)
	}
	return total
}
