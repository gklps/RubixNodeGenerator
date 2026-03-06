package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// savedConfig mirrors the fields written by writeConfig() in main.go.
// Used to reconstruct the node list without re-running the full setup.
type savedConfig struct {
	Branch      string `json:"branch"`
	InstallPath string `json:"install_path"`
	Network     string `json:"network"`
	QuorumNodes int    `json:"quorum_nodes"`
	NormalNodes int    `json:"normal_nodes"`
	BasePort    int    `json:"base_port"`
	BaseGrpc    int    `json:"base_grpc"`
	PortGap     int    `json:"port_gap"`
}

// ─────────────────────────────────────────────
// runKillAllNodes is the entry point for `./rubix-setup --killAllNode`.
//
// It:
//  1. Locates config.json in installPath (or the executable's directory)
//  2. Reconstructs the node list
//  3. Sends a graceful `rubixgoplatform shutdown -port <port>` to each node
//  4. Kills the screen sessions (Unix) or prints instructions (Windows)
//
// installPath may be "" — in that case the function searches known locations.
// ─────────────────────────────────────────────
func runKillAllNodes(installPath string) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║               RUBIX SETUP — Shutdown All Nodes           ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")

	// ── 1. Locate and read config.json ────────────────────────────────────────
	cfgPath, err := findConfigJSON(installPath)
	if err != nil {
		printWarn(fmt.Sprintf("Could not find config.json: %v", err))
		printWarn("Falling back to process-kill only.")
		killByProcessName()
		killAllScreenSessions()
		return
	}

	sc, err := loadSavedConfig(cfgPath)
	if err != nil {
		printWarn(fmt.Sprintf("Could not parse config.json: %v", err))
		killByProcessName()
		killAllScreenSessions()
		return
	}

	printProgress(fmt.Sprintf("Found config: %s", cfgPath))
	printProgress(fmt.Sprintf("Install path: %s", sc.InstallPath))
	printProgress(fmt.Sprintf("Nodes: %dQ + %dN, portGap=%d", sc.QuorumNodes, sc.NormalNodes, sc.PortGap))

	// ── 2. Rebuild node list from saved config ────────────────────────────────
	fakeCfg := &Config{
		QuorumNodes:  sc.QuorumNodes,
		NormalNodes:  sc.NormalNodes,
		PortGap:      sc.PortGap,
		BasePort:     sc.BasePort,
		BaseGrpcPort: sc.BaseGrpc,
	}
	nodes := buildNodePlan(fakeCfg)

	// ── 3. Graceful shutdown per node ─────────────────────────────────────────
	fmt.Println()
	printProgress(fmt.Sprintf("Sending shutdown signal to %d node(s)...", len(nodes)))

	for _, n := range nodes {
		shutdownNode(sc.InstallPath, n.Name, n.Port)
	}

	// Give nodes a moment to exit cleanly before killing sessions
	time.Sleep(2 * time.Second)

	// ── 4. Kill screen sessions (or print Windows instructions) ───────────────
	killSessionsForNodes(nodes)

	fmt.Println()
	printSuccess("All shutdown signals sent.")
	fmt.Println()
	fmt.Println("  If any processes remain, run:")
	if runtime.GOOS == "windows" {
		fmt.Println(`    Stop-Process -Name rubixgoplatform -Force`)
	} else {
		fmt.Println("    pkill -f rubixgoplatform")
	}
}

// ─────────────────────────────────────────────
// shutdownNode sends `rubixgoplatform shutdown -port <port>` to one node.
// A failed shutdown (e.g. node already stopped) prints a warning but does
// not abort — remaining nodes still need to be shut down.
// ─────────────────────────────────────────────
func shutdownNode(installPath, nodeName string, port int) {
	printProgress(fmt.Sprintf("  Shutting down %s (port=%d) ...", nodeName, port))

	binary := filepath.Join(installPath, executableName("rubixgoplatform"))
	cmd := exec.Command(binary, "shutdown", "-port", fmt.Sprintf("%d", port))
	cmd.Dir = installPath
	out, err := cmd.CombinedOutput()

	if err != nil {
		printWarn(fmt.Sprintf("    %s: shutdown error (%v) — may already be stopped.", nodeName, err))
		if len(out) > 0 {
			printWarn(fmt.Sprintf("    Output: %s", truncate(string(out), 120)))
		}
		return
	}
	printSuccess(fmt.Sprintf("    %s: shutdown accepted.", nodeName))
}

// ─────────────────────────────────────────────
// killSessionsForNodes kills the screen session (or nohup process) for
// each node by name. On Windows, it prints a PowerShell snippet instead.
// ─────────────────────────────────────────────
func killSessionsForNodes(nodes []nodeInfo) {
	if runtime.GOOS == "windows" {
		fmt.Println()
		fmt.Println("  On Windows, close the background processes from Task Manager")
		fmt.Println("  or run:")
		fmt.Println(`    Stop-Process -Name rubixgoplatform -Force`)
		return
	}

	fmt.Println()
	printProgress("Killing screen sessions...")
	for _, n := range nodes {
		killScreenSession(n.Name)
	}
}

// killScreenSession terminates a named screen session.
// Failure is silently ignored (session may not exist).
func killScreenSession(sessionName string) {
	cmd := exec.Command("screen", "-S", sessionName, "-X", "quit")
	_ = cmd.Run() // ignore error — session may already be gone
	printProgress(fmt.Sprintf("  Killed screen session: %s", sessionName))
}

// killAllScreenSessions is a fallback when no config.json is found.
// It kills any screen session whose name matches quorum/node patterns.
func killAllScreenSessions() {
	if runtime.GOOS == "windows" {
		return
	}
	printProgress("Killing all rubix screen sessions (pattern: quorum*, node*)...")
	// List sessions and kill matches
	out, err := exec.Command("screen", "-ls").CombinedOutput()
	if err != nil {
		return
	}
	for _, line := range splitLines(string(out)) {
		// screen -ls lines look like: "	12345.quorum1	(Detached)"
		name := extractScreenName(line)
		if name == "" {
			continue
		}
		if matchesNodePattern(name) {
			killScreenSession(name)
		}
	}
}

// killByProcessName kills all rubixgoplatform processes by name.
// Used as a last-resort fallback when config.json cannot be found.
func killByProcessName() {
	if runtime.GOOS == "windows" {
		printProgress("Stopping rubixgoplatform processes (Windows)...")
		cmd := exec.Command("powershell", "-NoProfile", "-Command",
			`Stop-Process -Name rubixgoplatform -Force -ErrorAction SilentlyContinue`)
		_ = cmd.Run()
		return
	}
	printProgress("Killing all rubixgoplatform processes (pkill fallback)...")
	cmd := exec.Command("pkill", "-f", "rubixgoplatform")
	_ = cmd.Run()
}

// ─────────────────────────────────────────────
// Config file helpers
// ─────────────────────────────────────────────

// findConfigJSON searches for config.json in the following order:
//  1. <installPath>/config.json          (if installPath is given)
//  2. <executable directory>/config.json
//  3. <current working directory>/config.json
func findConfigJSON(installPath string) (string, error) {
	candidates := []string{}

	if installPath != "" {
		candidates = append(candidates, filepath.Join(installPath, "config.json"))
	}
	candidates = append(candidates,
		filepath.Join(getExecutableDir(), "config.json"),
		filepath.Join(getCurrentDir(), "config.json"),
	)

	for _, p := range candidates {
		if fileExists(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("config.json not found in any of: %v", candidates)
}

// loadSavedConfig reads and parses a config.json written by writeConfig().
func loadSavedConfig(path string) (*savedConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sc savedConfig
	if err := json.Unmarshal(data, &sc); err != nil {
		return nil, err
	}
	// Apply defaults for older config.json that may lack these fields
	if sc.BasePort == 0 {
		sc.BasePort = 20000
	}
	if sc.BaseGrpc == 0 {
		sc.BaseGrpc = 10500
	}
	if sc.PortGap == 0 {
		sc.PortGap = 10
	}
	return &sc, nil
}

// ─────────────────────────────────────────────
// String helpers (avoids importing regexp)
// ─────────────────────────────────────────────

// splitLines splits s into non-empty, trimmed lines.
func splitLines(s string) []string {
	var out []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			line := trimSpace(s[start:i])
			if line != "" {
				out = append(out, line)
			}
			start = i + 1
		}
	}
	if line := trimSpace(s[start:]); line != "" {
		out = append(out, line)
	}
	return out
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

// extractScreenName extracts the session name from a `screen -ls` output line.
// Input example: "	12345.quorum1	(Detached)"
// Output: "quorum1"
func extractScreenName(line string) string {
	// Find the first field (tab-separated or space-separated)
	line = trimSpace(line)
	// Get first word
	end := len(line)
	for i, c := range line {
		if c == '\t' || c == ' ' {
			end = i
			break
		}
	}
	word := line[:end] // e.g. "12345.quorum1"
	// Strip PID prefix (everything up to and including the first '.')
	for i, c := range word {
		if c == '.' {
			return word[i+1:]
		}
	}
	return ""
}

// matchesNodePattern returns true if the session name looks like a rubix node.
// Matches: quorum1, quorum2, ..., node1, node2, ...
func matchesNodePattern(name string) bool {
	if len(name) == 0 {
		return false
	}
	prefixes := []string{"quorum", "node"}
	for _, p := range prefixes {
		if len(name) > len(p) && name[:len(p)] == p {
			// Rest must be a digit string
			rest := name[len(p):]
			allDigits := true
			for _, c := range rest {
				if c < '0' || c > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				return true
			}
		}
	}
	return false
}
