package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
)

type repoContext struct {
	Ecosystem string
	Alias     string
	RepoName  string
	BareRepo  string
}

type worktreeContext struct {
	RepoContext  repoContext
	Branch       string
	SafeBranch   string
	WorktreePath string
	SymlinkName  string
	SymlinkPath  string
}

type currentHydraContext struct {
	RepoContext  repoContext
	Branch       string
	WorktreePath string
	SymlinkPath  string
}

type branchChoice struct {
	Name             string
	DisplayName      string
	HasWorktree      bool
	WorktreeName     string
	WorktreeLinkPath string
	IsRemoteDefault  bool
}

func resolveRepoByAlias(cfg *config.Config, projectRoot, alias string) (repoContext, error) {
	for ecosystem, group := range cfg.Ecosystems {
		if repoName, ok := group[alias]; ok {
			return repoContext{
				Ecosystem: ecosystem,
				Alias:     alias,
				RepoName:  repoName,
				BareRepo:  filepath.Join(projectRoot, cfg.Paths.BareDir, repoName+".git"),
			}, nil
		}
	}
	return repoContext{}, fmt.Errorf("unknown alias: %s", alias)
}

func resolveCurrentHydraContext(wd string, cfg *config.Config, projectRoot string) (*currentHydraContext, error) {
	for ecosystem, group := range cfg.Ecosystems {
		for alias, repoName := range group {
			symlinkDir := filepath.Join(projectRoot, ecosystem)
			entries, err := os.ReadDir(symlinkDir)
			if err != nil {
				continue
			}

			for _, entry := range entries {
				if entry.Type()&os.ModeSymlink == 0 {
					continue
				}

				symlinkPath := filepath.Join(symlinkDir, entry.Name())
				realPath, err := filepath.EvalSymlinks(symlinkPath)
				if err != nil {
					continue
				}
				if !isWithinPath(wd, realPath) {
					continue
				}

				branch, err := git.GetCurrentBranch(realPath)
				if err != nil {
					branch = branchFromSymlinkName(alias, entry.Name())
				}

				return &currentHydraContext{
					RepoContext: repoContext{
						Ecosystem: ecosystem,
						Alias:     alias,
						RepoName:  repoName,
						BareRepo:  filepath.Join(projectRoot, cfg.Paths.BareDir, repoName+".git"),
					},
					Branch:       branch,
					WorktreePath: realPath,
					SymlinkPath:  symlinkPath,
				}, nil
			}
		}
	}

	return nil, nil
}

func collectWorktreeChoices(cfg *config.Config, projectRoot string) ([]worktreeContext, error) {
	var items []worktreeContext

	for ecosystem, group := range cfg.Ecosystems {
		for alias, repoName := range group {
			repo := repoContext{
				Ecosystem: ecosystem,
				Alias:     alias,
				RepoName:  repoName,
				BareRepo:  filepath.Join(projectRoot, cfg.Paths.BareDir, repoName+".git"),
			}

			choices, err := listRepoWorktrees(repo, projectRoot)
			if err != nil {
				continue
			}
			items = append(items, choices...)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].RepoContext.Ecosystem != items[j].RepoContext.Ecosystem {
			return items[i].RepoContext.Ecosystem < items[j].RepoContext.Ecosystem
		}
		return items[i].SymlinkName < items[j].SymlinkName
	})

	return items, nil
}

func listRepoWorktrees(repo repoContext, projectRoot string) ([]worktreeContext, error) {
	worktrees, err := git.ListWorktrees(repo.BareRepo)
	if err != nil {
		return nil, err
	}

	items := make([]worktreeContext, 0, len(worktrees))
	for _, wt := range worktrees {
		if wt.IsBare {
			continue
		}
		branch := wt.Branch
		if branch == "" {
			branch = "detached"
		}
		item := buildWorktreeContext(repo, projectRoot, branch)
		item.WorktreePath = wt.Path
		items = append(items, item)
	}

	return items, nil
}

func buildWorktreeContext(repo repoContext, projectRoot, branch string) worktreeContext {
	safeBranch := normalizeBranchName(branch)
	symlinkName := symlinkNameForBranch(repo.Alias, safeBranch)
	symlinkPath := filepath.Join(projectRoot, repo.Ecosystem, symlinkName)
	worktreePath := filepath.Join(repo.BareRepo, safeBranch)

	return worktreeContext{
		RepoContext:  repo,
		Branch:       branch,
		SafeBranch:   safeBranch,
		WorktreePath: worktreePath,
		SymlinkName:  symlinkName,
		SymlinkPath:  symlinkPath,
	}
}

func normalizeBranchName(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

func symlinkNameForBranch(alias, safeBranch string) string {
	if safeBranch == "main" || safeBranch == "master" {
		return alias
	}
	return alias + "-" + safeBranch
}

func branchFromSymlinkName(alias, symlinkName string) string {
	if symlinkName == alias {
		return "main"
	}
	prefix := alias + "-"
	if strings.HasPrefix(symlinkName, prefix) {
		return strings.TrimPrefix(symlinkName, prefix)
	}
	return symlinkName
}

func findWorktreeByName(cfg *config.Config, projectRoot, name string) (worktreeContext, bool) {
	items, err := collectWorktreeChoices(cfg, projectRoot)
	if err != nil {
		return worktreeContext{}, false
	}

	query := strings.ToLower(name)
	for _, item := range items {
		candidates := []string{
			item.SymlinkName,
			item.Branch,
			normalizeBranchName(item.Branch),
			fmt.Sprintf("%s/%s", item.RepoContext.Ecosystem, item.SymlinkName),
		}
		for _, candidate := range candidates {
			if strings.EqualFold(candidate, query) {
				return item, true
			}
		}
	}

	return worktreeContext{}, false
}

func findSimilarWorktreesByName(cfg *config.Config, projectRoot, query string) []string {
	items, err := collectWorktreeChoices(cfg, projectRoot)
	if err != nil {
		return nil
	}

	var matches []string
	needle := strings.ToLower(query)
	for _, item := range items {
		name := fmt.Sprintf("%s/%s", item.RepoContext.Ecosystem, item.SymlinkName)
		if strings.Contains(strings.ToLower(name), needle) || strings.Contains(needle, strings.ToLower(item.SymlinkName)) {
			matches = append(matches, name)
		}
	}
	if len(matches) > 5 {
		matches = matches[:5]
	}
	return matches
}

func navigationHints(wd string, wt worktreeContext) (string, string) {
	cdPath := wt.SymlinkPath
	if rel, err := filepath.Rel(wd, wt.SymlinkPath); err == nil && rel != "" {
		cdPath = rel
	}
	return "cd " + cdPath, "hydra switch " + wt.SymlinkName
}

func branchChoicesForRepo(repo repoContext, projectRoot string) ([]branchChoice, string, error) {
	branches, err := git.GetRemoteBranchesFromBare(repo.BareRepo)
	if err != nil {
		return nil, "", err
	}

	worktrees, err := listRepoWorktrees(repo, projectRoot)
	if err != nil {
		return nil, "", err
	}

	worktreeByBranch := make(map[string]worktreeContext, len(worktrees))
	for _, wt := range worktrees {
		worktreeByBranch[wt.Branch] = wt
	}

	defaultBranch, err := git.GetRemoteDefaultBranch(repo.BareRepo)
	if err != nil || defaultBranch == "" {
		defaultBranch = git.GetDefaultBranch(branches)
	}

	choices := make([]branchChoice, 0, len(branches))
	seen := make(map[string]struct{}, len(branches))
	for _, branch := range branches {
		if _, ok := seen[branch.Name]; ok {
			continue
		}
		seen[branch.Name] = struct{}{}
		choice := branchChoice{
			Name:            branch.Name,
			DisplayName:     branch.Name,
			IsRemoteDefault: branch.Name == defaultBranch,
		}
		if wt, ok := worktreeByBranch[branch.Name]; ok {
			choice.HasWorktree = true
			choice.WorktreeName = wt.SymlinkName
			choice.WorktreeLinkPath = wt.SymlinkPath
			choice.DisplayName = fmt.Sprintf("%s (worktree: %s)", branch.Name, wt.SymlinkName)
		}
		if choice.IsRemoteDefault {
			choice.DisplayName += " (default)"
		}
		choices = append(choices, choice)
	}

	sort.Slice(choices, func(i, j int) bool {
		if choices[i].IsRemoteDefault != choices[j].IsRemoteDefault {
			return choices[i].IsRemoteDefault
		}
		return choices[i].Name < choices[j].Name
	})

	return choices, defaultBranch, nil
}

func ensureSymlink(wt worktreeContext) error {
	if err := os.MkdirAll(filepath.Dir(wt.SymlinkPath), 0755); err != nil {
		return err
	}
	if existing, err := os.Readlink(wt.SymlinkPath); err == nil {
		relTarget, relErr := filepath.Rel(filepath.Dir(wt.SymlinkPath), wt.WorktreePath)
		if relErr == nil && existing == relTarget {
			return nil
		}
		if removeErr := os.Remove(wt.SymlinkPath); removeErr != nil {
			return removeErr
		}
	}
	relPath, err := filepath.Rel(filepath.Dir(wt.SymlinkPath), wt.WorktreePath)
	if err != nil {
		return err
	}
	if err := os.Symlink(relPath, wt.SymlinkPath); err != nil && !os.IsExist(err) {
		return err
	}
	return nil
}

func isWithinPath(wd, root string) bool {
	wd = filepath.Clean(wd)
	root = filepath.Clean(root)
	if wd == root {
		return true
	}
	sep := string(os.PathSeparator)
	return strings.HasPrefix(wd, root+sep)
}
