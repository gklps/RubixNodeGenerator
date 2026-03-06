package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ─────────────────────────────────────────────
// Config holds all settings resolved from flags or interactive prompts.
// ─────────────────────────────────────────────
type Config struct {
	// Provided via flags or prompts
	AutoMode    bool
	Branch      string
	InstallPath string
	TestNet     bool
	QuorumNodes int
	NormalNodes int
	PortGap     int

	// Action flags (checked before running setup)
	KillAllNodes bool // --killAllNode: gracefully shutdown all running nodes

	// Derived / fixed
	BasePort     int
	BaseGrpcPort int
}

func main() {
	// ─── Subcommand: status ───────────────────────────────────────────────────
	if len(os.Args) > 1 && os.Args[1] == "status" {
		runStatusCommand()
		os.Exit(0)
	}

	cfg := parseFlags()

	// ─── Action: --killAllNode ────────────────────────────────────────────────
	if cfg.KillAllNodes {
		runKillAllNodes(cfg.InstallPath)
		os.Exit(0)
	}

	printBanner()

	// ─── Session mode: New or Resume ─────────────────────────────────────────
	mode := promptSessionMode(cfg)
	if mode == "resume" {
		resumeSession(cfg)
		os.Exit(0)
	}

	// ─── Step 1: Branch Selection & Clone ────────────────────────────────────
	printStep(1, "Branch Selection & Clone")
	repoPath, installPath, err := setupGit(cfg)
	if err != nil {
		fatal("Git setup failed", err)
	}
	cfg.InstallPath = installPath

	// ─── Step 2: Build rubixgoplatform ───────────────────────────────────────
	printStep(2, "Build rubixgoplatform")
	if err := buildProject(repoPath, installPath, cfg); err != nil {
		fatal("Build failed", err)
	}

	// ─── Step 3: Download IPFS Kubo v0.19.1 ──────────────────────────────────
	printStep(3, "Download IPFS Kubo v0.19.1")
	if err := downloadIPFS(installPath); err != nil {
		fatal("IPFS download failed", err)
	}

	// ─── Step 4: Copy Swarm Keys ──────────────────────────────────────────────
	printStep(4, "Copy Swarm Keys")
	if err := copySwarmKeys(repoPath, installPath); err != nil {
		fatal("Swarm key copy failed", err)
	}

	// ─── Steps 5 & 6: Network & Node Setup + Launch ───────────────────────────
	printStep(5, "Network & Node Setup")
	nodes, err := setupNodes(installPath, cfg)
	if err != nil {
		fatal("Node setup failed", err)
	}

	// ─── Step 6: DID Creation & Quorum Setup ─────────────────────────────────
	printStep(6, "DID Creation & Quorum Setup")
	if len(nodes) > 0 {
		if err := setupDIDs(installPath, nodes, cfg); err != nil {
			printWarn(fmt.Sprintf("DID setup encountered errors: %v", err))
		}
	} else {
		printWarn("Node launch was cancelled or no nodes configured — skipping DID setup.")
	}

	// ─── Step 7: Summary ─────────────────────────────────────────────────────
	printStep(7, "Setup Complete")
	printSummary(cfg)

	// Write config.json to install directory
	if err := writeConfig(cfg); err != nil {
		printWarn(fmt.Sprintf("Could not write config.json: %v", err))
	}
}

// ─────────────────────────────────────────────
// parseFlags parses CLI flags and populates a Config.
// Any value left at its zero value will be collected interactively later.
// ─────────────────────────────────────────────
func parseFlags() *Config {
	cfg := &Config{
		BasePort:     20000,
		BaseGrpcPort: 10500,
	}

	flag.BoolVar(&cfg.AutoMode, "auto", false,
		"Non-interactive mode: use all defaults without prompting")
	flag.StringVar(&cfg.Branch, "branch", "",
		"Branch to clone (e.g. main, development)")
	flag.StringVar(&cfg.InstallPath, "install-path", "",
		"Directory where rubix binaries are installed (used with --killAllNode too)")
	flag.BoolVar(&cfg.TestNet, "testnet", false,
		"Use testnet instead of mainnet")
	flag.IntVar(&cfg.QuorumNodes, "quorum-nodes", 0,
		"Number of quorum nodes to create (0 = prompt)")
	flag.IntVar(&cfg.NormalNodes, "normal-nodes", 0,
		"Number of normal nodes to create (0 = prompt)")
	flag.IntVar(&cfg.PortGap, "port-gap", 0,
		"Port gap between nodes (0 = prompt, default 10)")
	flag.BoolVar(&cfg.KillAllNodes, "killAllNode", false,
		"Gracefully shutdown all running nodes using rubixgoplatform shutdown")

	flag.Parse()

	// In auto mode, fill in defaults for anything not specified by flags
	if cfg.AutoMode {
		applyAutoDefaults(cfg)
	}

	return cfg
}

// applyAutoDefaults fills in defaults for auto mode.
func applyAutoDefaults(cfg *Config) {
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	if cfg.InstallPath == "" {
		cfg.InstallPath = getExecutableDir()
	}
	if cfg.QuorumNodes == 0 {
		cfg.QuorumNodes = 5
	}
	if cfg.NormalNodes == 0 {
		cfg.NormalNodes = 5
	}
	if cfg.PortGap == 0 {
		cfg.PortGap = 10
	}
}

// ─────────────────────────────────────────────
// printSummary prints the final configuration table.
// ─────────────────────────────────────────────
func printSummary(cfg *Config) {
	network := "Mainnet"
	if cfg.TestNet {
		network = "Testnet"
	}

	totalNodes := cfg.QuorumNodes + cfg.NormalNodes
	lastPort := cfg.BasePort
	if totalNodes > 0 {
		lastPort = cfg.BasePort + ((totalNodes - 1) * cfg.PortGap)
	}

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║                     SETUP SUMMARY                        ║")
	fmt.Println("╠══════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Branch          : %-37s ║\n", cfg.Branch)
	fmt.Printf("║  Install dir     : %-37s ║\n", truncate(cfg.InstallPath, 37))
	fmt.Printf("║  Network         : %-37s ║\n", network)
	fmt.Printf("║  Quorum nodes    : %-37d ║\n", cfg.QuorumNodes)
	fmt.Printf("║  Normal nodes    : %-37d ║\n", cfg.NormalNodes)
	fmt.Printf("║  Base port       : %-37d ║\n", cfg.BasePort)
	fmt.Printf("║  Base gRPC port  : %-37d ║\n", cfg.BaseGrpcPort)
	fmt.Printf("║  Port gap        : %-37d ║\n", cfg.PortGap)
	fmt.Printf("║  Port range      : %d – %-30d ║\n", cfg.BasePort, lastPort)
	fmt.Printf("║  Platform        : %-37s ║\n", runtime.GOOS+"/"+runtime.GOARCH)
	fmt.Println("╚══════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Setup completed successfully.")
	fmt.Println()
}

// ─────────────────────────────────────────────
// writeConfig serialises the Config to config.json in the install directory.
// ─────────────────────────────────────────────
func writeConfig(cfg *Config) error {
	network := "mainnet"
	if cfg.TestNet {
		network = "testnet"
	}

	data := map[string]interface{}{
		"branch":       cfg.Branch,
		"install_path": cfg.InstallPath,
		"network":      network,
		"quorum_nodes": cfg.QuorumNodes,
		"normal_nodes": cfg.NormalNodes,
		"base_port":    cfg.BasePort,
		"base_grpc":    cfg.BaseGrpcPort,
		"port_gap":     cfg.PortGap,
		"platform":     runtime.GOOS + "/" + runtime.GOARCH,
		"ipfs_version": ipfsVersion,
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	dst := filepath.Join(cfg.InstallPath, "config.json")
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return err
	}
	printSuccess(fmt.Sprintf("config.json written to %s", dst))
	return nil
}

// ─────────────────────────────────────────────
// runStatusCommand lists running rubix screen sessions (Linux/macOS)
// or prints instructions for Windows.
// ─────────────────────────────────────────────
func runStatusCommand() {
	fmt.Println("=== Rubix Node Status ===")

	switch runtime.GOOS {
	case "windows":
		fmt.Println("On Windows, check running processes with:")
		fmt.Println(`  Get-Process | Where-Object { $_.Name -like "*rubix*" }`)

	default: // linux, darwin
		out, err := shellOutput("bash", "-c", "screen -ls 2>/dev/null | grep -E '\\.(quorum|node)[0-9]'")
		if err != nil || strings.TrimSpace(out) == "" {
			fmt.Println("No active rubix screen sessions found.")
			fmt.Println("(Ensure screen is installed and nodes were started with rubix-setup.)")
			return
		}
		fmt.Println("Active screen sessions:")
		fmt.Println(out)
	}
}

// shellOutput runs a command and returns its combined stdout as a string.
func shellOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	return string(out), err
}

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

// fatal prints an error message and exits with code 1.
func fatal(context string, err error) {
	fmt.Fprintf(os.Stderr, "\n[ERROR] %s: %v\n", context, err)
	os.Exit(1)
}

// truncate shortens s to maxLen characters, adding "…" if trimmed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
