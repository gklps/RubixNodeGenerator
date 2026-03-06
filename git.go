package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const rubixRepoURL = "https://github.com/rubixchain/rubixgoplatform"
const githubAPIBranches = "https://api.github.com/repos/rubixchain/rubixgoplatform/branches?per_page=100"

// branchInfo is the minimal structure returned by the GitHub branches API.
type branchInfo struct {
	Name string `json:"name"`
}

// ─────────────────────────────────────────────
// fetchBranches fetches all branch names from GitHub.
// Returns at least ["main", "development"] even on API failure.
// ─────────────────────────────────────────────
func fetchBranches() ([]string, error) {
	printProgress("Fetching branch list from GitHub...")

	client := &http.Client{}
	req, err := http.NewRequest("GET", githubAPIBranches, nil)
	if err != nil {
		return defaultBranches(), nil
	}
	// GitHub API prefers an Accept header; also helps avoid rate-limit issues.
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "rubix-setup/1.0")

	resp, err := client.Do(req)
	if err != nil {
		printWarn("Could not reach GitHub API; using default branch list.")
		return defaultBranches(), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		printWarn(fmt.Sprintf("GitHub API returned %d; using default branch list.", resp.StatusCode))
		return defaultBranches(), nil
	}

	var branches []branchInfo
	if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
		printWarn("Could not parse GitHub API response; using default branch list.")
		return defaultBranches(), nil
	}

	names := make([]string, 0, len(branches))
	for _, b := range branches {
		names = append(names, b.Name)
	}
	if len(names) == 0 {
		return defaultBranches(), nil
	}
	return names, nil
}

// defaultBranches returns the fallback branch list.
func defaultBranches() []string {
	return []string{"main", "development"}
}

// ─────────────────────────────────────────────
// promptBranchSelection shows a numbered branch list and returns the chosen branch.
// ─────────────────────────────────────────────
func promptBranchSelection(branches []string, cfg *Config) string {
	// If flag was already set, skip prompt.
	if cfg.Branch != "" {
		printProgress(fmt.Sprintf("Using branch from flag: %s", cfg.Branch))
		return cfg.Branch
	}

	fmt.Println("\nAvailable branches:")
	for i, b := range branches {
		fmt.Printf("  %d - %s\n", i, b)
	}
	fmt.Println()
	fmt.Println("  Enter a number to select, OR type a custom branch name.")
	fmt.Printf("  (Press Enter to use default: 0 - %s)\n", branches[0])

	for {
		raw := readLine("\nYour selection: ")

		// Empty → default (index 0)
		if raw == "" {
			printSuccess(fmt.Sprintf("Selected branch: %s", branches[0]))
			return branches[0]
		}

		// Try to parse as integer index
		idx := -1
		_, err := fmt.Sscanf(raw, "%d", &idx)
		if err == nil {
			if idx >= 0 && idx < len(branches) {
				printSuccess(fmt.Sprintf("Selected branch: %s", branches[idx]))
				return branches[idx]
			}
			fmt.Printf("    Invalid index %d. Valid range: 0–%d\n", idx, len(branches)-1)
			continue
		}

		// Treat as a custom branch name
		printSuccess(fmt.Sprintf("Using custom branch: %s", raw))
		return raw
	}
}

// ─────────────────────────────────────────────
// promptInstallPath asks the user where to install binaries.
// Default is the directory of the running executable.
// Any relative path (e.g. "test") is resolved to an absolute path
// based on the current working directory.
// ─────────────────────────────────────────────
func promptInstallPath(cfg *Config) string {
	if cfg.InstallPath != "" {
		abs := absPath(cfg.InstallPath)
		printProgress(fmt.Sprintf("Using install path from flag: %s", abs))
		return abs
	}

	defaultPath := getExecutableDir()

	fmt.Println("\nWhere should rubix binaries be installed?")
	fmt.Printf("  Default: %s\n", defaultPath)
	fmt.Println("  Example: /home/user/rubix  OR  C:\\rubix")
	fmt.Println("  Tip    : You can type a relative path like \"test\" and it will")
	fmt.Printf("           resolve to %s/test\n", getCurrentDir())

	raw := readLine("\nEnter install path (Press Enter for default): ")
	if raw == "" {
		printSuccess(fmt.Sprintf("Using default install path: %s", defaultPath))
		return defaultPath
	}

	// Resolve relative paths to absolute immediately so all subsequent
	// operations use a stable, unambiguous path.
	abs := absPath(raw)
	printSuccess(fmt.Sprintf("Using install path: %s", abs))
	return abs
}

// ─────────────────────────────────────────────
// setupGit orchestrates Step 1: branch selection, path selection, clone/update.
// Returns (repoPath, installPath, error).
// repoPath is the local path to the cloned rubixgoplatform repository.
// ─────────────────────────────────────────────
func setupGit(cfg *Config) (repoPath string, installPath string, err error) {
	// 1a. Fetch and display branches
	branches, _ := fetchBranches()
	branch := promptBranchSelection(branches, cfg)
	cfg.Branch = branch

	// 1b. Get install path
	installPath = promptInstallPath(cfg)
	cfg.InstallPath = installPath

	if err := ensureDir(installPath); err != nil {
		return "", "", fmt.Errorf("cannot create install directory %q: %w", installPath, err)
	}

	// 1c. Determine clone path.
	// Use "rubixgoplatform-src" so it does not collide with the
	// rubixgoplatform binary that will be copied into installPath.
	repoPath = filepath.Join(installPath, "rubixgoplatform-src")

	// 1d. Clone or update
	if err := cloneOrUpdateRepo(branch, repoPath); err != nil {
		return "", "", err
	}

	return repoPath, installPath, nil
}

// ─────────────────────────────────────────────
// cloneOrUpdateRepo clones the repo at the given branch into repoPath.
// If repoPath already contains a git repository, the user is asked whether
// to pull latest changes or skip.
// ─────────────────────────────────────────────
func cloneOrUpdateRepo(branch, repoPath string) error {
	gitDir := filepath.Join(repoPath, ".git")

	if fileExists(gitDir) {
		// Repo already present
		fmt.Printf("\nDirectory %q already contains a git repository.\n", repoPath)
		fmt.Println("  1 - Pull latest changes from remote")
		fmt.Println("  2 - Skip (use existing files as-is)")

		choice := readInt("\nYour choice [1/2]: ", 1, 2)
		if choice == 2 {
			printProgress("Skipping clone; using existing repository.")
			return nil
		}
		return pullRepo(repoPath)
	}

	return cloneRepo(branch, repoPath)
}

// cloneRepo runs: git clone -b <branch> --depth 1 <url> <repoPath>
func cloneRepo(branch, repoPath string) error {
	printProgress(fmt.Sprintf("Cloning branch '%s' into %s ...", branch, repoPath))

	if err := ensureDir(filepath.Dir(repoPath)); err != nil {
		return fmt.Errorf("prepare parent directory: %w", err)
	}

	args := []string{
		"clone",
		"-b", branch,
		"--depth", "1",
		rubixRepoURL,
		repoPath,
	}

	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	printSuccess("Repository cloned successfully.")
	return nil
}

// pullRepo runs: git pull inside the existing repo directory.
func pullRepo(repoPath string) error {
	printProgress(fmt.Sprintf("Pulling latest changes in %s ...", repoPath))

	cmd := exec.Command("git", "pull")
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
	}

	printSuccess("Repository updated successfully.")
	return nil
}

// sanitizeBranchName strips unsafe characters from a branch name for display.
// (Not used for security-critical operations — git handles that.)
func sanitizeBranchName(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '/' || r == '.' {
			return r
		}
		return '-'
	}, name)
}
