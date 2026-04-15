package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var glossaryCmd = &cobra.Command{
	Use:   "glossary",
	Short: "Show glossary of Hydra terms",
	Long:  "Display explanations for all Hydra terminology and concepts in an interactive view.",
	RunE:  runGlossary,
}

// GlossaryEntry represents a single glossary entry
type GlossaryEntry struct {
	Term       string
	Definition string
	Examples   []string
}

func (e GlossaryEntry) Title() string       { return e.Term }
func (e GlossaryEntry) Description() string { return "" }
func (e GlossaryEntry) FilterValue() string { return e.Term }

var glossaryEntries = []GlossaryEntry{
	{
		Term:       "Group",
		Definition: "A category that organizes related repositories. Groups help you navigate between different parts of your project.",
		Examples:   []string{"backend (APIs, services)", "frontend (web apps)", "infra (Terraform, Docker configs)"},
	},
	{
		Term:       "Alias",
		Definition: "A short name for a repository within its group. This is how you refer to and navigate to the repository.",
		Examples:   []string{"cd backend/api", "hydra sync worker"},
	},
	{
		Term:       "Worktree",
		Definition: "A Git feature that allows multiple working directories from a single repository. Each branch can have its own worktree.",
		Examples:   []string{"main/ - production code", "develop/ - active development", "feature/login/ - new feature"},
	},
	{
		Term:       "Bare Repository",
		Definition: "A Git repository without a working directory. It stores all Git history and is kept in .bare/. All worktrees share this single source.",
		Examples:   []string{"Stored in: .bare/my-repo.git/"},
	},
	{
		Term:       "Hydra Project",
		Definition: "A directory containing a .hydra.yaml configuration file. This is the root where Hydra manages all your repositories.",
		Examples:   []string{"Contains: .hydra.yaml, .bare/, group folders"},
	},
}

func init() {
	rootCmd.AddCommand(glossaryCmd)
}

// glossaryModel represents the state of the glossary TUI
type glossaryModel struct {
	list     list.Model
	detail   GlossaryEntry
	width    int
	height   int
	quitting bool
}

func newGlossaryModel() glossaryModel {
	// Convert entries to list items
	items := make([]list.Item, len(glossaryEntries))
	for i, entry := range glossaryEntries {
		items[i] = entry
	}

	// Create list
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Terms"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7aa2f7"))
	l.Styles.PaginationStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#565f89"))
	l.Styles.HelpStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#565f89"))

	// Custom key bindings
	l.KeyMap = list.KeyMap{
		GoToStart:            key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g/home", "go to start")),
		GoToEnd:              key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G/end", "go to end")),
		NextPage:             key.NewBinding(key.WithKeys("right", "l", "pgdown"), key.WithHelp("→/l/pgdn", "next page")),
		PrevPage:             key.NewBinding(key.WithKeys("left", "h", "pgup"), key.WithHelp("←/h/pgup", "prev page")),
		Filter:               key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		ClearFilter:          key.NewBinding(key.WithKeys("esc")),
		CancelWhileFiltering: key.NewBinding(key.WithKeys("esc")),
		AcceptWhileFiltering: key.NewBinding(key.WithKeys("enter", "tab")),
	}

	return glossaryModel{
		list:   l,
		detail: glossaryEntries[0],
	}
}

func (m glossaryModel) Init() tea.Cmd {
	return nil
}

func (m glossaryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Set list width to 30% of screen, min 20, max 30
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
			// Update detail view when item selected
			if i, ok := m.list.SelectedItem().(GlossaryEntry); ok {
				m.detail = i
			}
		}
	}

	// Update list
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)

	// Update detail when selection changes
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

	// Calculate widths
	listWidth := int(float64(m.width) * 0.3)
	if listWidth < 20 {
		listWidth = 20
	}
	if listWidth > 30 {
		listWidth = 30
	}
	detailWidth := m.width - listWidth - 4

	// Header
	header := styles.AppHeader.Render(" HYDRA ")
	title := styles.Title.Render("Glossary")

	// Build detail view
	termStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7aa2f7")).
		MarginBottom(1)

	descStyle := lipgloss.NewStyle().
		Foreground(styles.Fg).
		MarginBottom(1)

	exampleStyle := lipgloss.NewStyle().
		Foreground(styles.FgComment).
		MarginTop(1)

	// Build examples
	var examples strings.Builder
	if len(m.detail.Examples) > 0 {
		examples.WriteString("\nExamples:\n")
		for _, ex := range m.detail.Examples {
			examples.WriteString(fmt.Sprintf("  • %s\n", ex))
		}
	}

	detailContent := fmt.Sprintf("%s\n%s\n%s",
		termStyle.Render(m.detail.Term),
		descStyle.Render(m.detail.Definition),
		exampleStyle.Render(examples.String()),
	)

	detailBox := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#24283b")).
		Width(detailWidth).
		Height(m.height - 8).
		Render(detailContent)

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#565f89")).
		MarginTop(1)
	help := helpStyle.Render("↑/↓: navigate • enter/space: select • q: quit")

	// Layout
	listView := lipgloss.NewStyle().
		Width(listWidth).
		Render(m.list.View())

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		listView,
		"  ",
		detailBox,
	)

	return fmt.Sprintf("%s\n\n%s\n\n%s\n\n%s",
		header,
		title,
		content,
		help,
	)
}

func runGlossary(cmd *cobra.Command, args []string) error {
	model := newGlossaryModel()

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}

	return nil
}
