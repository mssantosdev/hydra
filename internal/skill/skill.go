// Package skill emits hydra's embedded agent skill and parses it, so a drift test
// can prove the shipped skill still describes the binary that ships it.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mssantosdev/hydra/skills"
)

// DefaultInstallDir is where a consuming workspace expects agent skills.
const DefaultInstallDir = ".agents/skills"

// Name is the skill's directory and frontmatter name.
const Name = "hydra"

// Content returns the embedded SKILL.md verbatim.
func Content() string { return skills.SkillMD }

// Install writes the skill to <dir>/hydra/SKILL.md and returns the written path.
// It overwrites unconditionally: the embedded copy is authoritative.
func Install(dir string) (string, error) {
	if dir == "" {
		dir = DefaultInstallDir
	}
	target := filepath.Join(dir, Name)
	if err := os.MkdirAll(target, 0750); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", target, err)
	}
	path := filepath.Join(target, "SKILL.md")
	//nolint:gosec // G306: SKILL.md is committed to the consuming repo; it must stay readable
	if err := os.WriteFile(path, []byte(Content()), 0644); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", path, err)
	}
	return path, nil
}

// tableRow matches a markdown table row, capturing the cells between pipes.
var tableRow = regexp.MustCompile(`^\|(.+)\|$`)

// backticked matches the `code` span a table's first cell uses.
var backticked = regexp.MustCompile("^`([^`]+)`$")

// section returns the lines of a "## <title>" section.
func section(title string) []string {
	var out []string
	inside := false
	for _, line := range strings.Split(Content(), "\n") {
		if strings.HasPrefix(line, "## ") {
			inside = strings.TrimSpace(strings.TrimPrefix(line, "## ")) == title
			continue
		}
		if inside {
			out = append(out, line)
		}
	}
	return out
}

func rowCells(line string) []string {
	match := tableRow.FindStringSubmatch(strings.TrimSpace(line))
	if match == nil {
		return nil
	}
	cells := strings.Split(match[1], "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// DocumentedCommands returns the command names listed in the Commands table.
func DocumentedCommands() []string {
	var names []string
	for _, line := range section("Commands") {
		cells := rowCells(line)
		if len(cells) < 3 {
			continue
		}
		match := backticked.FindStringSubmatch(cells[0])
		if match == nil {
			continue
		}
		names = append(names, match[1])
	}
	return names
}

// DocumentedErrorCodes returns the code -> exit-code map from the Contract table.
func DocumentedErrorCodes() map[string]int {
	codes := make(map[string]int)
	// "Error codes", not "Contract": the envelope and the code table were one section
	// and are now two, so this reads the one that actually holds the table.
	for _, line := range section("Error codes") {
		cells := rowCells(line)
		if len(cells) < 3 {
			continue
		}
		match := backticked.FindStringSubmatch(cells[0])
		if match == nil {
			continue
		}
		exit, err := strconv.Atoi(cells[1])
		if err != nil {
			continue
		}
		codes[match[1]] = exit
	}
	return codes
}
