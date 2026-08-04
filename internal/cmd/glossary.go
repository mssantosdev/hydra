package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var glossaryCmd = &cobra.Command{
	Use:   "glossary",
	Short: "Show glossary of Hydra terms",
	Long: `Interactive glossary of Hydra terminology and concepts.

DESCRIPTION
  Explains Hydra-specific terms for the workspace model:
    - Project    - Root directory with .hydra/config.yaml
    - Group      - Category organizing related repositories
    - Repo       - Registered repository (alias) within a group
    - Worktree   - Real sibling directory checked out from a repo
    - Bare repo  - Git data store under .bare/ (no working tree)
    - Upstream   - Remote tracking branch for ahead/behind status
    - JSON output - Machine-readable envelope with --output json

  In an interactive terminal, opens a TUI. Otherwise emits the glossary as data.

WHEN TO USE
  - New to Hydra - learn the terminology
  - Understanding the project -> group -> repo -> worktree model
  - Before running other commands
  - When docs reference unfamiliar terms

EXAMPLES
  # Open interactive glossary (TTY)
  $ hydra glossary

  # Emit glossary as JSON (non-TTY or --output json)
  $ hydra glossary --output json

NAVIGATION (interactive)
  Up/Down or j/k     Navigate terms
  Enter/Space        View term details
  q / Esc / Ctrl+c   Quit

EXIT CODES
  0  Success
  1  General error (terminal error)

SEE ALSO
  - hydra init - Create your first Hydra project
  - hydra clone - Add your first repository
  - hydra skill - Agent contract and JSON envelope
  - Docs: https://github.com/mssantosdev/hydra/blob/main/docs/README.md`,
	RunE: runGlossary,
}

type GlossaryEntry struct {
	Term       string
	Definition string
	Examples   []string
}

func (e GlossaryEntry) Title() string       { return e.Term }
func (e GlossaryEntry) Description() string { return "" }
func (e GlossaryEntry) FilterValue() string { return e.Term }

type glossaryTerm struct {
	Term       string `json:"term"`
	Definition string `json:"definition"`
}

type glossaryPayload struct {
	Terms []glossaryTerm `json:"terms"`
}

var glossaryEntries = []GlossaryEntry{
	{
		Term:       "Project",
		Definition: "The root directory that contains a .hydra/config.yaml file. Hydra walks up from your current directory to find it and treats everything under that root as one workspace.",
		Examples: []string{
			"my-app/ with .hydra/config.yaml at the root",
			"Registered in ~/.config/hydra/projects.yaml for --project lookups",
		},
	},
	{
		Term:       "Group",
		Definition: "A named category that organizes related repositories inside a project. Groups become top-level folders next to .bare/, and every repo alias lives under exactly one group.",
		Examples: []string{
			"backend (APIs and services)",
			"frontend (web apps)",
			"infra (Terraform, Docker configs)",
		},
	},
	{
		Term:       "Repo",
		Definition: "A repository registered in .hydra/config.yaml under a group. The map key is the alias — Hydra's short handle for the repo in commands like add, remove, and sync.",
		Examples: []string{
			"hydra add api feature/login",
			"hydra sync worker",
		},
	},
	{
		Term:       "Worktree",
		Definition: "A real sibling directory checked out from a repo's bare repository. Each worktree maps to one branch (or detached HEAD) and is how you work on multiple branches at once.",
		Examples: []string{
			"backend/api/ for the default branch",
			"backend/api-feature-login/ for a feature branch",
		},
	},
	{
		Term:       "Bare Repository",
		Definition: "The git object store under .bare/<alias>.git/. It holds history and refs only — never a working tree. Every worktree for that repo shares this single bare repo.",
		Examples: []string{
			".bare/api.git/ stores all git data for alias api",
			"Worktrees live as real directories under their group",
		},
	},
	{
		Term:       "Upstream Tracking",
		Definition: "The remote branch a local branch tracks (for example origin/main). Hydra reports ahead/behind counts and dirty status per worktree using this tracking information.",
		Examples: []string{
			"status shows ~3 when three files changed",
			"JSON worktree objects include upstream, ahead, and behind",
		},
	},
	{
		Term:       "JSON Output",
		Definition: "Hydra's machine-readable contract. With --output json (or when stdout is not a terminal), commands emit a versioned JSON envelope instead of styled text so scripts never scrape prose.",
		Examples: []string{
			"hydra list --output json",
			"hydra glossary --output json -> {terms: [{term, definition}]}",
		},
	},
}

func init() {
	rootCmd.AddCommand(glossaryCmd)
}

type glossaryModel struct {
	list     list.Model
	detail   GlossaryEntry
	width    int
	height   int
	quitting bool
}

func newGlossaryModel() glossaryModel {
	items := make([]list.Item, len(glossaryEntries))
	for i, entry := range glossaryEntries {
		items[i] = entry
	}

	l := list.New(items, list.NewDefaultDelegate(), 20, 10)
	l.Title = "Terms"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(styles.Blue)
	l.Styles.PaginationStyle = lipgloss.NewStyle().Foreground(styles.FgComment)
	l.Styles.HelpStyle = lipgloss.NewStyle().Foreground(styles.FgComment)
	l.KeyMap = list.DefaultKeyMap()

	return glossaryModel{
		list:   l,
		detail: glossaryEntries[0],
	}
}

func (m glossaryModel) Init() tea.Cmd { return nil }

func (m glossaryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		listWidth := int(float64(msg.Width) * 0.3)
		if listWidth < 20 {
			listWidth = 20
		}
		if listWidth > 30 {
			listWidth = 30
		}
		m.list.SetSize(listWidth, msg.Height-4)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "enter", " ":
			if i, ok := m.list.SelectedItem().(GlossaryEntry); ok {
				m.detail = i
			}
			return m, nil
		case "up", "k":
			m.list.CursorUp()
			if i, ok := m.list.SelectedItem().(GlossaryEntry); ok {
				m.detail = i
			}
			return m, nil
		case "down", "j":
			m.list.CursorDown()
			if i, ok := m.list.SelectedItem().(GlossaryEntry); ok {
				m.detail = i
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	if i, ok := m.list.SelectedItem().(GlossaryEntry); ok {
		m.detail = i
	}
	return m, cmd
}

func (m glossaryModel) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		return "Loading..."
	}

	listWidth := int(float64(m.width) * 0.3)
	if listWidth < 20 {
		listWidth = 20
	}
	if listWidth > 30 {
		listWidth = 30
	}
	detailWidth := m.width - listWidth - 4

	header := styles.AppHeader.Render(" HYDRA ")
	title := styles.Title.Render("Glossary")

	termStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Blue).MarginBottom(1)
	descStyle := lipgloss.NewStyle().Foreground(styles.Fg).MarginBottom(1)
	exampleStyle := lipgloss.NewStyle().Foreground(styles.FgComment).MarginTop(1)

	var examples strings.Builder
	if len(m.detail.Examples) > 0 {
		examples.WriteString("\nExamples:\n")
		for _, ex := range m.detail.Examples {
			_, _ = fmt.Fprintf(&examples, "  - %s\n", ex)
		}
	}

	detailContent := fmt.Sprintf("%s\n%s\n%s",
		termStyle.Render(m.detail.Term),
		descStyle.Render(m.detail.Definition),
		exampleStyle.Render(examples.String()),
	)

	detailBox := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(styles.BgLight).
		Width(detailWidth).
		Height(m.height - 8).
		Render(detailContent)

	help := lipgloss.NewStyle().Foreground(styles.FgComment).MarginTop(1).
		Render("up/down: navigate - enter/space: select - q: quit")

	listView := lipgloss.NewStyle().Width(listWidth).Render(m.list.View())
	content := lipgloss.JoinHorizontal(lipgloss.Top, listView, "  ", detailBox)

	return fmt.Sprintf("%s\n\n%s\n\n%s\n\n%s", header, title, content, help)
}

func glossaryData() glossaryPayload {
	terms := make([]glossaryTerm, len(glossaryEntries))
	for i, entry := range glossaryEntries {
		terms[i] = glossaryTerm{Term: entry.Term, Definition: entry.Definition}
	}
	return glossaryPayload{Terms: terms}
}

func renderGlossaryText() string {
	var b strings.Builder
	for i, entry := range glossaryEntries {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(entry.Term)
		b.WriteString("\n")
		b.WriteString(entry.Definition)
	}
	return b.String()
}

func runGlossary(cmd *cobra.Command, args []string) error {
	if !interactive() {
		data := glossaryData()
		text := renderGlossaryText()
		return emit(cmd, data, nil, func() {
			_, _ = fmt.Fprint(cmd.OutOrStdout(), text)
			if !strings.HasSuffix(text, "\n") {
				_, _ = fmt.Fprintln(cmd.OutOrStdout())
			}
		})
	}

	model := newGlossaryModel()
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return output.Wrap(output.CodeInternal, err, "glossary TUI failed")
	}
	return nil
}
