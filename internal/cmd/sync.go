package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/log"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync [alias]",
	Short: "Pull latest changes for worktrees",
	Long: `Check remote for updates and pull changes to selected worktrees.

DESCRIPTION
  Fetches remote updates and pulls them into local worktrees.
  Handles dirty worktrees by stashing changes (with confirmation).

  By default:
    • Inside a worktree: syncs current repository
    • At project root: syncs all repositories

WHEN TO USE
  • Morning routine - get latest changes
  • Before starting work on a feature
  • Keeping staging/production worktrees updated
  • After a teammate merges to shared branch

EXAMPLES
  # Sync current repository
  $ hydra sync

  # Sync specific repository
  $ hydra sync api

  # Sync all repositories
  $ hydra sync --all

  # Non-interactive mode (pull all clean worktrees)
  $ hydra sync --yes

  # Force pull dirty worktrees (stash, pull, restore)
  $ hydra sync --yes --force

FLAGS
  -a, --all     Sync all repositories
  -y, --yes     Skip confirmation, pull all clean worktrees
  -f, --force   Pull dirty worktrees (stash changes first)
  -h, --help    Show help

EXIT CODES
  0  Success (all selected worktrees synced)
  1  General error or sync failures
  2  Config file (.hydra.yaml) not found

SEE ALSO
  • hydra status - Check which worktrees have updates
  • hydra add - Create worktrees from updated branches
  • Docs: https://github.com/mssantosdev/hydra/blob/main/docs/commands/worktree-management.md`,
	RunE: runSync,
}

var (
	syncAll   bool
	syncYes   bool
	syncForce bool
)

// SyncWorktree represents a worktree that can be synced
type SyncWorktree struct {
	Repo        string
	Alias       string
	Group       string
	Branch      string
	Path        string
	BareRepo    string
	HasChanges  bool
	ChangeCount int
	Behind      int
	Ahead       int
	Selected    bool
	Status      string // "clean", "dirty", "error"
}

// SyncResult represents the result of a sync operation
type SyncResult struct {
	Worktree SyncWorktree
	Success  bool
	Error    error
	Action   string // "pulled", "stashed", "skipped", "failed"
	Duration time.Duration
}

func init() {
	rootCmd.AddCommand(syncCmd)

	syncCmd.Flags().BoolVarP(&syncAll, "all", "a", false, "Sync all repositories")
	syncCmd.Flags().BoolVarP(&syncYes, "yes", "y", false, "Skip confirmation, pull all clean worktrees")
	syncCmd.Flags().BoolVarP(&syncForce, "force", "f", false, "Pull dirty worktrees (stash changes)")
}

func runSync(cmd *cobra.Command, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	configPath, cfg, err := config.FindConfig(wd)
	if err != nil {
		return err
	}

	projectRoot := filepath.Dir(configPath)

	// Determine which repositories to sync
	var targetAlias string
	if len(args) > 0 {
		targetAlias = args[0]
	} else if !syncAll {
		// Try to detect current repository from working directory
		targetAlias = detectCurrentAlias(wd, cfg, projectRoot)
	}

	// Collect worktrees to sync
	worktrees, err := collectWorktrees(cfg, projectRoot, targetAlias)
	if err != nil {
		return err
	}

	if len(worktrees) == 0 {
		log.Info("No worktrees found to sync")
		return nil
	}

	// Check remote for updates
	log.Info("Checking for remote updates...")
	worktrees = checkForUpdates(worktrees)

	// Filter to only worktrees with updates
	worktreesWithUpdates := filterWithUpdates(worktrees)
	if len(worktreesWithUpdates) == 0 {
		log.Success("All worktrees are up to date")
		return nil
	}

	log.Info(fmt.Sprintf("Found %d worktrees with available updates", len(worktreesWithUpdates)))

	// Interactive selection (unless --yes)
	if !syncYes {
		worktreesWithUpdates = selectWorktreesToSync(worktreesWithUpdates)
		if len(worktreesWithUpdates) == 0 {
			log.Info("No worktrees selected for sync")
			return nil
		}
	} else {
		// In --yes mode, auto-select clean worktrees
		for i := range worktreesWithUpdates {
			if !worktreesWithUpdates[i].HasChanges {
				worktreesWithUpdates[i].Selected = true
			}
		}
	}

	// Handle dirty worktrees
	if !syncForce {
		worktreesWithUpdates = handleDirtyWorktrees(worktreesWithUpdates)
	}

	// Execute sync
	results := executeSync(worktreesWithUpdates, syncForce)

	// Print results
	printSyncResults(results)

	return nil
}

func detectCurrentAlias(wd string, cfg *config.Config, projectRoot string) string {
	// Check if we're inside a worktree
	for groupName, group := range cfg.Ecosystems {
		for alias := range group {
			// Check common paths
			symlinkDir := filepath.Join(projectRoot, groupName)

			for _, suffix := range []string{"", "-main", "-master", "-stage", "-prod"} {
				linkPath := filepath.Join(symlinkDir, alias+suffix)
				if strings.HasPrefix(wd, linkPath) {
					return alias
				}
			}
		}
	}
	return ""
}

func collectWorktrees(cfg *config.Config, projectRoot, targetAlias string) ([]SyncWorktree, error) {
	var worktrees []SyncWorktree

	for groupName, group := range cfg.Ecosystems {
		for alias, repoName := range group {
			if targetAlias != "" && alias != targetAlias {
				continue
			}

			bareRepo := filepath.Join(projectRoot, cfg.Paths.BareDir, repoName+".git")

			if _, err := os.Stat(bareRepo); os.IsNotExist(err) {
				continue
			}

			wtList, err := git.ListWorktrees(bareRepo)
			if err != nil {
				log.Debug("Failed to list worktrees", "repo", alias, "error", err)
				continue
			}

			for _, wt := range wtList {
				if wt.IsBare {
					continue
				}

				branch := wt.Branch
				if branch == "" {
					branch = "detached"
				}

				worktrees = append(worktrees, SyncWorktree{
					Repo:     repoName,
					Alias:    alias,
					Group:    groupName,
					Branch:   branch,
					Path:     wt.Path,
					BareRepo: bareRepo,
				})
			}
		}
	}

	return worktrees, nil
}

func checkForUpdates(worktrees []SyncWorktree) []SyncWorktree {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5) // Limit concurrent checks

	for i := range worktrees {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			wt := &worktrees[idx]

			// Check for uncommitted changes
			hasChanges, count, err := git.HasUncommittedChanges(wt.Path)
			if err == nil {
				wt.HasChanges = hasChanges
				wt.ChangeCount = count
			}

			// Check commits behind/ahead
			status, err := git.CheckWorktreeStatus(wt.BareRepo, wt.Path, wt.Branch)
			if err == nil {
				wt.Behind = status.CommitsBehind
				wt.Ahead = status.CommitsAhead
			}

			// Set status
			if wt.HasChanges {
				wt.Status = "dirty"
			} else if wt.Behind > 0 {
				wt.Status = "clean"
			} else {
				wt.Status = "clean"
			}
		}(i)
	}

	wg.Wait()
	return worktrees
}

func filterWithUpdates(worktrees []SyncWorktree) []SyncWorktree {
	var result []SyncWorktree
	for _, wt := range worktrees {
		if wt.Behind > 0 {
			result = append(result, wt)
		}
	}
	return result
}

func selectWorktreesToSync(worktrees []SyncWorktree) []SyncWorktree {
	// Build table view
	fmt.Println()
	fmt.Println(styles.Title.Render("Worktrees with Available Updates"))
	fmt.Println()

	// Show table header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7aa2f7"))

	fmt.Printf("  %s  %-15s %-15s %-8s %-12s\n",
		headerStyle.Render("Select"),
		headerStyle.Render("Repository"),
		headerStyle.Render("Branch"),
		headerStyle.Render("Behind"),
		headerStyle.Render("Status"))
	fmt.Println(strings.Repeat("─", 70))

	// Pre-select clean worktrees
	var defaultSelected []string
	for _, wt := range worktrees {
		if !wt.HasChanges {
			defaultSelected = append(defaultSelected, wtKey(wt))
		}
	}

	// Build options
	var options []huh.Option[string]
	for _, wt := range worktrees {
		label := fmt.Sprintf("%-15s %-15s %-8d",
			wt.Alias,
			wt.Branch,
			wt.Behind)

		if wt.HasChanges {
			label += fmt.Sprintf(" ⚠ %d changes", wt.ChangeCount)
		} else {
			label += " ✓ clean"
		}

		options = append(options, huh.NewOption(label, wtKey(wt)))
	}

	var selected []string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select worktrees to update").
				Description("Clean worktrees are pre-selected. Dirty worktrees require special handling.").
				Options(options...).
				Value(&selected),
		),
	)

	// Set defaults
	selected = defaultSelected

	if err := form.Run(); err != nil {
		return nil
	}

	// Mark selected worktrees
	selectedMap := make(map[string]bool)
	for _, key := range selected {
		selectedMap[key] = true
	}

	var result []SyncWorktree
	for _, wt := range worktrees {
		if selectedMap[wtKey(wt)] {
			wt.Selected = true
			result = append(result, wt)
		}
	}

	return result
}

func handleDirtyWorktrees(worktrees []SyncWorktree) []SyncWorktree {
	var result []SyncWorktree

	for _, wt := range worktrees {
		if !wt.HasChanges || !wt.Selected {
			result = append(result, wt)
			continue
		}

		// Ask user how to handle dirty worktree
		fmt.Println()
		fmt.Printf("Worktree '%s/%s' has %d uncommitted changes.\n\n",
			wt.Alias, wt.Branch, wt.ChangeCount)

		var action string
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("How would you like to proceed?").
					Options(
						huh.NewOption("Stash changes, pull, then restore", "stash"),
						huh.NewOption("Discard all changes (reset --hard)", "reset"),
						huh.NewOption("Skip this worktree", "skip"),
					).
					Value(&action),
			),
		)

		if err := form.Run(); err != nil {
			// Skip on error
			wt.Selected = false
			result = append(result, wt)
			continue
		}

		switch action {
		case "stash":
			wt.Status = "stash"
			result = append(result, wt)
		case "reset":
			wt.Status = "reset"
			result = append(result, wt)
		case "skip":
			wt.Selected = false
			result = append(result, wt)
		}
	}

	return result
}

func executeSync(worktrees []SyncWorktree, force bool) []SyncResult {
	var results []SyncResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Create result channel
	resultChan := make(chan SyncResult, len(worktrees))

	// Show progress header
	fmt.Println()
	fmt.Println(styles.Title.Render("Pulling Updates"))
	fmt.Println()

	// Execute syncs concurrently
	for _, wt := range worktrees {
		if !wt.Selected {
			continue
		}

		wg.Add(1)
		go func(wt SyncWorktree) {
			defer wg.Done()

			start := time.Now()
			result := SyncResult{
				Worktree: wt,
				Success:  true,
			}

			// Handle dirty worktree
			if wt.HasChanges {
				switch wt.Status {
				case "stash":
					if err := git.StashChanges(wt.Path); err != nil {
						result.Success = false
						result.Error = fmt.Errorf("failed to stash: %w", err)
						result.Action = "stash-failed"
						resultChan <- result
						return
					}
					result.Action = "stashed"

					// Pull
					if err := git.PullWorktree(wt.Path, wt.Branch); err != nil {
						result.Success = false
						result.Error = err
						result.Action = "pull-failed"
						resultChan <- result
						return
					}

					// Pop stash
					if err := git.PopStash(wt.Path); err != nil {
						result.Success = false
						result.Error = fmt.Errorf("pull succeeded but failed to restore stash: %w", err)
						result.Action = "pop-failed"
					} else {
						result.Action = "pulled-stashed"
					}

				case "reset":
					if err := git.ResetHard(wt.Path); err != nil {
						result.Success = false
						result.Error = fmt.Errorf("failed to reset: %w", err)
						result.Action = "reset-failed"
						resultChan <- result
						return
					}

					// Pull
					if err := git.PullWorktree(wt.Path, wt.Branch); err != nil {
						result.Success = false
						result.Error = err
						result.Action = "pull-failed"
					} else {
						result.Action = "pulled-reset"
					}
				}
			} else {
				// Clean worktree, just pull
				if err := git.PullWorktree(wt.Path, wt.Branch); err != nil {
					result.Success = false
					result.Error = err
					result.Action = "pull-failed"
				} else {
					result.Action = "pulled"
				}
			}

			result.Duration = time.Since(start)
			resultChan <- result
		}(wt)
	}

	// Close result channel when all done
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results with live display
	completed := 0
	total := len(worktrees)

	for result := range resultChan {
		mu.Lock()
		results = append(results, result)
		completed++
		mu.Unlock()

		// Show progress
		progress := float64(completed) / float64(total)
		barWidth := 30
		filled := int(progress * float64(barWidth))
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

		status := styles.Success.Render("✓")
		if !result.Success {
			status = styles.Error.Render("✗")
		}

		fmt.Printf("\r  %s [%s] %d/%d %s/%s",
			status,
			bar,
			completed,
			total,
			result.Worktree.Alias,
			result.Worktree.Branch)
	}

	fmt.Println() // New line after progress
	return results
}

func printSyncResults(results []SyncResult) {
	fmt.Println()
	fmt.Println(styles.Title.Render("Sync Results"))
	fmt.Println()

	// Success box
	successCount := 0
	failCount := 0

	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failCount++
		}
	}

	// Summary
	if successCount > 0 {
		fmt.Printf("  %s Successfully synced %d worktree(s)\n",
			styles.Success.Render("✓"), successCount)
	}
	if failCount > 0 {
		fmt.Printf("  %s Failed to sync %d worktree(s)\n",
			styles.Error.Render("✗"), failCount)
	}

	// Details
	if failCount > 0 {
		fmt.Println()
		fmt.Println(styles.Label.Render("Failed worktrees:"))
		for _, r := range results {
			if !r.Success {
				fmt.Printf("  • %s/%s: %s\n",
					r.Worktree.Alias,
					r.Worktree.Branch,
					r.Error)
			}
		}
	}

	fmt.Println()
}

func wtKey(wt SyncWorktree) string {
	return fmt.Sprintf("%s/%s", wt.Alias, wt.Branch)
}
