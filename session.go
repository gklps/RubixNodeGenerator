package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ─────────────────────────────────────────────
// promptSessionMode asks the user whether to start a new setup or resume
// an existing one.
//
// Smart auto-detect: if --install-path is given and that path already
// contains a config.json, we skip the question and suggest Resume.
//
// Returns "new" or "resume".
// ─────────────────────────────────────────────
func promptSessionMode(cfg *Config) string {
	// Auto-detect: --install-path was given and already has a session
	if cfg.InstallPath != "" {
		abs := absPath(cfg.InstallPath)
		if isExistingSession(abs) {
			fmt.Printf("\n[INFO] Existing rubix-setup session detected at: %s\n", abs)
			fmt.Println("  1 - Resume this session  (manage running nodes)")
			fmt.Println("  2 - New session          (clone, build, launch fresh)")
			choice := readInt("\nYour choice [1/2]: ", 1, 2)
			if choice == 1 {
				return "resume"
			}
			return "new"
		}
	}

	// No install path given (or path has no session) — ask
	fmt.Println()
	fmt.Println("  1 - New session    (clone, build, download IPFS, launch nodes)")
	fmt.Println("  2 - Resume session (manage nodes in an existing install path)")
	choice := readInt("\nYour choice [1/2]: ", 1, 2)
	if choice == 2 {
		return "resume"
	}
	return "new"
}

// isExistingSession returns true if the path contains a valid rubix-setup
// session, identified by the presence of config.json.
func isExistingSession(path string) bool {
	return fileExists(filepath.Join(path, "config.json"))
}

// ─────────────────────────────────────────────
// resumeSession is the entry point for Resume mode.
//
//  1. Asks for (or uses) the install path
//  2. Validates it is a rubix-setup session (has config.json)
//  3. Loads config.json and did_mapping.json
//  4. Launches the interactive command menu
//
// ─────────────────────────────────────────────
func resumeSession(cfg *Config) {
	// ── 1. Resolve install path ───────────────────────────────────────────────
	installPath := promptResumePath(cfg)
	if installPath == "" {
		printWarn("No install path entered. Exiting resume.")
		return
	}

	// ── 2. Validate session ───────────────────────────────────────────────────
	cfgPath, err := findConfigJSON(installPath)
	if err != nil {
		fmt.Printf("\n[ERROR] No rubix-setup session found at %q.\n", installPath)
		fmt.Println("  Ensure the path contains a config.json written by rubix-setup.")
		return
	}

	sc, err := loadSavedConfig(cfgPath)
	if err != nil {
		fmt.Printf("\n[ERROR] Could not read config.json: %v\n", err)
		return
	}

	// ── 3. Load DID mapping (optional — may not exist yet) ────────────────────
	var mapping *didMapping
	didPath := filepath.Join(sc.InstallPath, "did_mapping.json")
	if fileExists(didPath) {
		m, loadErr := loadDIDMapping(didPath)
		if loadErr != nil {
			printWarn(fmt.Sprintf("Could not load did_mapping.json: %v", loadErr))
		} else {
			mapping = m
		}
	}

	// ── 4. Show header and enter command menu ─────────────────────────────────
	printSessionHeader(sc, mapping)
	runCommandMenu(sc.InstallPath, sc, mapping)
}

// promptResumePath asks the user for the install path to resume.
// If cfg.InstallPath is already set (via --install-path flag), use it directly.
func promptResumePath(cfg *Config) string {
	if cfg.InstallPath != "" {
		abs := absPath(cfg.InstallPath)
		printProgress(fmt.Sprintf("Resuming session at: %s", abs))
		return abs
	}

	fmt.Println("\nEnter the install path of the existing rubix-setup session.")
	fmt.Println("  Example: test   OR   /home/user/rubix")
	raw := readLine("Install path: ")
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	return absPath(raw)
}

// printSessionHeader displays a summary of the resumed session.
func printSessionHeader(sc *savedConfig, mapping *didMapping) {
	network := sc.Network
	if network == "" {
		network = "unknown"
	}

	didStatus := "not loaded"
	if mapping != nil {
		didStatus = fmt.Sprintf("%d node(s) with DIDs", len(mapping.Nodes))
	}

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║               RUBIX SESSION — Command Menu               ║")
	fmt.Println("╠══════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Install path : %-40s║\n", truncate(sc.InstallPath, 40))
	fmt.Printf("║  Network      : %-40s║\n", network)
	fmt.Printf("║  Nodes        : %dQ + %dN, portGap=%-26d║\n",
		sc.QuorumNodes, sc.NormalNodes, sc.PortGap)
	fmt.Printf("║  DIDs         : %-40s║\n", didStatus)
	fmt.Println("╚══════════════════════════════════════════════════════════╝")
}

// ─────────────────────────────────────────────
// runCommandMenu is the interactive command loop for a resumed session.
// It loops until the user chooses to exit (0).
// Adding a new command: implement cmdXxx() and add a case here.
// ─────────────────────────────────────────────
func runCommandMenu(installPath string, sc *savedConfig, mapping *didMapping) {
	for {
		fmt.Println()
		fmt.Println("──────────────────────── Commands ─────────────────────────")
		fmt.Println("  1 - Generate test RBT tokens   (generatetestrbt)")
		fmt.Println("  2 - Get account info / balance (getaccountinfo)")
		fmt.Println("  3 - Setup quorum               (setupquorum)")
		fmt.Println("  4 - Start offline nodes        (relaunch stopped nodes)")
		fmt.Println("  5 - Show node status            (ports, screen sessions)")
		fmt.Println("  6 - Show DID mapping            (from did_mapping.json)")
		fmt.Println("  7 - Shutdown all nodes          (graceful shutdown)")
		fmt.Println("  0 - Exit")
		fmt.Println("────────────────────────────────────────────────────────────")

		choice := readInt("Your choice: ", 0, 7)

		switch choice {
		case 1:
			cmdGenerateTestRBT(installPath, mapping)
		case 2:
			cmdGetAccountInfo(installPath, mapping)
		case 3:
			cmdSetupQuorum(installPath, mapping)
		case 4:
			cmdStartNodes(sc)
		case 5:
			cmdShowStatus(installPath, sc)
		case 6:
			cmdShowDIDMapping(mapping)
		case 7:
			runKillAllNodes(installPath)
		case 0:
			fmt.Println("\nExiting session manager. Nodes continue running in the background.")
			return
		}
	}
}

// ─────────────────────────────────────────────
// nodeDidPair is a resolved (node, DID) pair ready for command execution.
// ─────────────────────────────────────────────
type nodeDidPair struct {
	Node nodeDIDRecord
	DID  string
}

// ─────────────────────────────────────────────
// promptNodeAndDID is a reusable helper used by all commands that need to
// target a specific node + DID combination.
//
// Flow:
//  1. Ask upfront: "All DIDs on all nodes" OR "Choose specific"
//     - All → collect every node+DID pair immediately, no further prompts
//     - Specific → show table, pick node(s), then per-node DID selection
//  2. If a chosen node has multiple DIDs → show sub-list, user picks one or all
//
// Returns a []nodeDidPair that the caller iterates over.
// ─────────────────────────────────────────────
func promptNodeAndDID(mapping *didMapping) []nodeDidPair {
	// ── Step 1: Upfront ALL vs Specific ──────────────────────────────────────
	fmt.Println()
	fmt.Println("  a - All DIDs on all nodes  (no further prompts)")
	fmt.Println("  b - Choose specific nodes/DIDs")
	raw := strings.ToLower(strings.TrimSpace(readLine("\n  Your choice [a/b]: ")))

	// ── Option A: everything, no prompts ─────────────────────────────────────
	if raw == "a" {
		var pairs []nodeDidPair
		for _, node := range mapping.Nodes {
			if len(node.DIDs) == 0 {
				printWarn(fmt.Sprintf("  %s: no DIDs available — skipping.", node.Name))
				continue
			}
			for _, d := range node.DIDs {
				pairs = append(pairs, nodeDidPair{Node: node, DID: d})
			}
		}
		printProgress(fmt.Sprintf("Selected all %d DID(s) across %d node(s).", len(pairs), len(mapping.Nodes)))
		return pairs
	}

	// ── Option B: specific node + DID selection ───────────────────────────────
	fmt.Println()
	printDIDTable(mapping)

	fmt.Println()
	fmt.Printf("  Enter node number (1–%d), or 0 for ALL nodes: ", len(mapping.Nodes))
	nodeChoice := readInt("", 0, len(mapping.Nodes))

	var selected []nodeDIDRecord
	if nodeChoice == 0 {
		selected = mapping.Nodes
	} else {
		selected = []nodeDIDRecord{mapping.Nodes[nodeChoice-1]}
	}

	var pairs []nodeDidPair
	for _, node := range selected {
		allDIDs := node.DIDs
		if len(allDIDs) == 0 {
			printWarn(fmt.Sprintf("  %s: no DIDs available — skipping.", node.Name))
			continue
		}

		if len(allDIDs) == 1 {
			// Only one DID — no sub-prompt needed
			pairs = append(pairs, nodeDidPair{Node: node, DID: allDIDs[0]})
			continue
		}

		// Multiple DIDs — let the user choose per node
		fmt.Printf("\n  Node %s has %d DID(s):\n", node.Name, len(allDIDs))
		for i, d := range allDIDs {
			label := ""
			if d == node.QuorumDID {
				label = "  ← quorum DID"
			}
			fmt.Printf("    %d - %s%s\n", i+1, d, label)
		}
		fmt.Printf("    0 - All DIDs on %s\n", node.Name)

		didChoice := readInt("  Your choice: ", 0, len(allDIDs))
		if didChoice == 0 {
			for _, d := range allDIDs {
				pairs = append(pairs, nodeDidPair{Node: node, DID: d})
			}
		} else {
			pairs = append(pairs, nodeDidPair{Node: node, DID: allDIDs[didChoice-1]})
		}
	}

	return pairs
}

// shortDID returns a truncated DID string for display purposes.
func shortDID(did string) string {
	if len(did) > 24 {
		return did[:24] + "..."
	}
	return did
}

// ─────────────────────────────────────────────
// cmdGenerateTestRBT runs:
//
//	./rubixgoplatform generatetestrbt -numTokens <n> -did <did> -port <port>
//
// Uses promptNodeAndDID so the user can pick any node+DID combination.
// ─────────────────────────────────────────────
func cmdGenerateTestRBT(installPath string, mapping *didMapping) {
	fmt.Println()
	fmt.Println("═══ Generate Test RBT Tokens ═══")

	if mapping == nil || len(mapping.Nodes) == 0 {
		printWarn("No DID mapping loaded. Cannot determine node DIDs.")
		printWarn("Tip: Ensure did_mapping.json exists in the install path.")
		return
	}

	pairs := promptNodeAndDID(mapping)
	if len(pairs) == 0 {
		printWarn("No node+DID pairs selected.")
		return
	}

	numTokens := readIntDefault(
		"\n  Number of tokens to generate (Press Enter for default 1): ",
		1, 10000, 1,
	)

	fmt.Println()
	printProgress(fmt.Sprintf("Generating %d token(s) on %d DID(s)...", numTokens, len(pairs)))
	for _, p := range pairs {
		runGenerateTestRBT(installPath, p, numTokens)
	}
}

// runGenerateTestRBT executes generatetestrbt for a single nodeDidPair.
func runGenerateTestRBT(installPath string, p nodeDidPair, numTokens int) {
	printProgress(fmt.Sprintf("  %s | DID: %s | tokens: %d",
		p.Node.Name, shortDID(p.DID), numTokens))

	out, err := runRubixCmd(installPath,
		"generatetestrbt",
		"-numTokens", fmt.Sprintf("%d", numTokens),
		"-did", p.DID,
		"-port", fmt.Sprintf("%d", p.Node.HTTPPort),
	)

	if err != nil {
		printWarn(fmt.Sprintf("  %s: command error — %v", p.Node.Name, err))
		if strings.TrimSpace(out) != "" {
			fmt.Printf("    Output: %s\n", strings.TrimSpace(out))
		}
		return
	}
	printSuccess(fmt.Sprintf("  %s: %s", p.Node.Name, strings.TrimSpace(out)))
}

// ─────────────────────────────────────────────
// cmdGetAccountInfo runs:
//
//	./rubixgoplatform getaccountinfo -did <did> -port <port>
//
// Offers two sub-modes:
//   - Single: pick node+DID interactively
//   - All:    query every DID across all nodes and print a summary table
//
// ─────────────────────────────────────────────
func cmdGetAccountInfo(installPath string, mapping *didMapping) {
	fmt.Println()
	fmt.Println("═══ Get Account Info / Balance ═══")

	if mapping == nil || len(mapping.Nodes) == 0 {
		printWarn("No DID mapping loaded. Cannot determine node DIDs.")
		return
	}

	fmt.Println()
	fmt.Println("  a - Single node/DID  (pick a specific node and DID)")
	fmt.Println("  b - All nodes table  (show every DID's balance in one table)")
	fmt.Println()
	raw := strings.ToLower(strings.TrimSpace(readLine("  Your choice [a/b]: ")))

	if raw == "b" {
		cmdGetAllBalances(installPath, mapping)
	} else {
		cmdGetSingleBalance(installPath, mapping)
	}
}

// cmdGetSingleBalance queries the balance for one user-selected node+DID.
func cmdGetSingleBalance(installPath string, mapping *didMapping) {
	pairs := promptNodeAndDID(mapping)
	if len(pairs) == 0 {
		printWarn("No node+DID pairs selected.")
		return
	}
	fmt.Println()
	for _, p := range pairs {
		printAccountInfo(installPath, p)
	}
}

// cmdGetAllBalances queries every DID on every node and renders a summary table.
func cmdGetAllBalances(installPath string, mapping *didMapping) {
	type balanceRow struct {
		NodeName string
		Port     int
		DID      string
		RBT      string
		Locked   string
		Pledged  string
		Pinned   string
		Err      string
	}

	fmt.Println()
	printProgress(fmt.Sprintf("Querying balance for all DIDs across %d node(s)...", len(mapping.Nodes)))

	var rows []balanceRow
	for _, node := range mapping.Nodes {
		for _, did := range node.DIDs {
			out, err := runRubixCmd(installPath,
				"getaccountinfo",
				"-did", did,
				"-port", fmt.Sprintf("%d", node.HTTPPort),
			)
			row := balanceRow{
				NodeName: node.Name,
				Port:     node.HTTPPort,
				DID:      did,
			}
			if err != nil {
				row.Err = err.Error()
			} else {
				row.RBT, row.Locked, row.Pledged, row.Pinned = parseAccountInfo(out)
			}
			rows = append(rows, row)
		}
	}

	// Print table
	fmt.Println()
	fmt.Println("  ┌─────────────┬──────────┬──────────────────────────┬──────────┬──────────┐")
	fmt.Println("  │ Node        │ Port     │ DID                      │ RBT      │ Locked   │")
	fmt.Println("  ├─────────────┼──────────┼──────────────────────────┼──────────┼──────────┤")
	for _, r := range rows {
		if r.Err != "" {
			fmt.Printf("  │ %-11s │ %-8d │ %-24s │ %-8s │ %-8s │\n",
				r.NodeName, r.Port, truncate(r.DID, 24), "ERROR", "-")
		} else {
			fmt.Printf("  │ %-11s │ %-8d │ %-24s │ %-8s │ %-8s │\n",
				r.NodeName, r.Port, truncate(r.DID, 24), r.RBT, r.Locked)
		}
	}
	fmt.Println("  └─────────────┴──────────┴──────────────────────────┴──────────┴──────────┘")
}

// printAccountInfo queries and pretty-prints balance for one nodeDidPair.
func printAccountInfo(installPath string, p nodeDidPair) {
	printProgress(fmt.Sprintf("  %s | DID: %s", p.Node.Name, shortDID(p.DID)))

	out, err := runRubixCmd(installPath,
		"getaccountinfo",
		"-did", p.DID,
		"-port", fmt.Sprintf("%d", p.Node.HTTPPort),
	)
	if err != nil {
		printWarn(fmt.Sprintf("  %s: error — %v", p.Node.Name, err))
		return
	}

	rbt, locked, pledged, pinned := parseAccountInfo(out)
	fmt.Printf("    DID     : %s\n", p.DID)
	fmt.Printf("    RBT     : %s\n", rbt)
	fmt.Printf("    Locked  : %s\n", locked)
	fmt.Printf("    Pledged : %s\n", pledged)
	fmt.Printf("    Pinned  : %s\n", pinned)
}

// parseAccountInfo extracts balance fields from rubixgoplatform getaccountinfo output.
//
// Expected line format:
//
//	RBT :     15.000, Locked RBT :      0.000, Pledged RBT :      0.000, Pinned RBT :      0.000
func parseAccountInfo(out string) (rbt, locked, pledged, pinned string) {
	rbt, locked, pledged, pinned = "?", "?", "?", "?"
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "RBT") {
			continue
		}
		// Parse comma-separated key : value pairs
		parts := strings.Split(line, ",")
		for _, part := range parts {
			kv := strings.SplitN(part, ":", 2)
			if len(kv) != 2 {
				continue
			}
			key := strings.TrimSpace(kv[0])
			val := strings.TrimSpace(kv[1])
			switch key {
			case "RBT":
				rbt = val
			case "Locked RBT":
				locked = val
			case "Pledged RBT":
				pledged = val
			case "Pinned RBT":
				pinned = val
			}
		}
		break
	}
	return
}

// ─────────────────────────────────────────────
// cmdSetupQuorum runs:
//
//	./rubixgoplatform setupquorum -did <did> -port <port>
//
// Only quorum nodes are shown in the selection. Any DID on the node can be
// used — it is not restricted to the primary/quorum DID.
// ─────────────────────────────────────────────
func cmdSetupQuorum(installPath string, mapping *didMapping) {
	fmt.Println()
	fmt.Println("═══ Setup Quorum ═══")

	if mapping == nil || len(mapping.Nodes) == 0 {
		printWarn("No DID mapping loaded.")
		return
	}

	// Build a filtered view that contains only quorum nodes
	quorumNodes := make([]nodeDIDRecord, 0)
	for _, n := range mapping.Nodes {
		if n.IsQuorum {
			quorumNodes = append(quorumNodes, n)
		}
	}
	if len(quorumNodes) == 0 {
		printWarn("No quorum nodes found in did_mapping.json.")
		return
	}

	quorumMapping := &didMapping{
		Network:   mapping.Network,
		CreatedAt: mapping.CreatedAt,
		Nodes:     quorumNodes,
	}

	fmt.Printf("  %d quorum node(s) available.\n", len(quorumNodes))
	pairs := promptNodeAndDID(quorumMapping)
	if len(pairs) == 0 {
		printWarn("No node+DID pairs selected.")
		return
	}

	fmt.Println()
	printProgress(fmt.Sprintf("Running setupquorum on %d DID(s)...", len(pairs)))
	for _, p := range pairs {
		runSetupQuorum(installPath, p)
	}
}

// runSetupQuorum executes setupquorum for a single nodeDidPair.
func runSetupQuorum(installPath string, p nodeDidPair) {
	shortD := shortDID(p.DID)
	printProgress(fmt.Sprintf("  %s | DID: %s", p.Node.Name, shortD))

	out, err := runRubixCmd(installPath,
		"setupquorum",
		"-did", p.DID,
		"-port", fmt.Sprintf("%d", p.Node.HTTPPort),
	)

	if err != nil {
		printWarn(fmt.Sprintf("  %s: command error — %v", p.Node.Name, err))
		if strings.TrimSpace(out) != "" {
			fmt.Printf("    Output: %s\n", strings.TrimSpace(out))
		}
		return
	}
	printSuccess(fmt.Sprintf("  %s: %s", p.Node.Name, strings.TrimSpace(out)))
}

// ─────────────────────────────────────────────
// cmdStartNodes checks each expected node and starts any that are offline.
//
// Logic per node:
//   - HTTP port NOT available (something is listening) → already running, skip
//   - HTTP port available (nothing listening)          → start the node
//
// ─────────────────────────────────────────────
func cmdStartNodes(sc *savedConfig) {
	fmt.Println()
	fmt.Println("═══ Start Offline Nodes ═══")

	// Reconstruct node plan from saved config
	fakeCfg := &Config{
		QuorumNodes:  sc.QuorumNodes,
		NormalNodes:  sc.NormalNodes,
		PortGap:      sc.PortGap,
		BasePort:     sc.BasePort,
		BaseGrpcPort: sc.BaseGrpc,
	}
	nodes := buildNodePlan(fakeCfg)
	testNet := sc.Network == "testnet"

	var alreadyRunning, started, failed []string

	fmt.Println()
	for _, n := range nodes {
		if !isPortAvailable(n.Port) {
			// Port is in use → node is running
			printSuccess(fmt.Sprintf("  %s (port=%d): already running", n.Name, n.Port))
			alreadyRunning = append(alreadyRunning, n.Name)
			continue
		}

		// Port is free → node is offline, start it
		printProgress(fmt.Sprintf("  %s (port=%d): offline — starting...", n.Name, n.Port))
		if err := launchNode(sc.InstallPath, n, testNet); err != nil {
			printWarn(fmt.Sprintf("  %s: failed to start — %v", n.Name, err))
			failed = append(failed, n.Name)
		} else {
			printSuccess(fmt.Sprintf("  %s: started. Log: %s.txt", n.Name, n.Name))
			started = append(started, n.Name)
		}
	}

	// Summary
	fmt.Println()
	fmt.Printf("  Summary: %d already running | %d started | %d failed\n",
		len(alreadyRunning), len(started), len(failed))
	if len(failed) > 0 {
		printWarn(fmt.Sprintf("  Failed nodes: %v", failed))
	}
}

// ─────────────────────────────────────────────
// cmdShowStatus prints running screen sessions and listening ports.
// ─────────────────────────────────────────────
func cmdShowStatus(installPath string, sc *savedConfig) {
	fmt.Println()
	fmt.Println("═══ Node Status ═══")

	// Reconstruct node plan from saved config
	fakeCfg := &Config{
		QuorumNodes:  sc.QuorumNodes,
		NormalNodes:  sc.NormalNodes,
		PortGap:      sc.PortGap,
		BasePort:     sc.BasePort,
		BaseGrpcPort: sc.BaseGrpc,
	}
	nodes := buildNodePlan(fakeCfg)

	fmt.Println()
	fmt.Println("  ┌─────────────┬──────────┬──────────┬────────────┐")
	fmt.Println("  │ Node        │ HTTP     │ gRPC     │ Screen     │")
	fmt.Println("  ├─────────────┼──────────┼──────────┼────────────┤")
	for _, n := range nodes {
		screen := screenSessionStatus(n.Name)
		fmt.Printf("  │ %-11s │ %-8d │ %-8d │ %-10s │\n",
			n.Name, n.Port, n.GrpcPort, screen)
	}
	fmt.Println("  └─────────────┴──────────┴──────────┴────────────┘")

	// Listening ports via lsof (Unix only)
	if runtime.GOOS != "windows" {
		fmt.Println()
		fmt.Println("  Listening TCP ports (rubixgoplatform processes):")
		out, err := exec.Command("bash", "-c",
			"lsof -iTCP -sTCP:LISTEN -nP 2>/dev/null | grep rubixgop | awk '{print $9}' | sort -t: -k2 -n",
		).Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				fmt.Printf("    %s\n", line)
			}
		} else {
			fmt.Println("    (none found — nodes may not be running)")
		}
	}
}

// screenSessionStatus checks if a named screen session is running.
// Returns "running", "stopped", or "unknown".
func screenSessionStatus(sessionName string) string {
	if runtime.GOOS == "windows" {
		return "N/A"
	}
	out, err := exec.Command("screen", "-ls").CombinedOutput()
	if err != nil && len(out) == 0 {
		return "unknown"
	}
	// screen -ls lines contain "<pid>.<sessionName>" — match the dot-prefixed name
	if strings.Contains(string(out), "."+sessionName+"\t") ||
		strings.Contains(string(out), "."+sessionName+" ") {
		return "running"
	}
	return "stopped"
}

// ─────────────────────────────────────────────
// cmdShowDIDMapping prints the full DID mapping table.
// ─────────────────────────────────────────────
func cmdShowDIDMapping(mapping *didMapping) {
	fmt.Println()
	fmt.Println("═══ DID Mapping ═══")

	if mapping == nil {
		printWarn("No did_mapping.json found for this session.")
		printWarn("Tip: Re-run setup, or check that did_mapping.json exists in the install path.")
		return
	}

	fmt.Printf("\n  Network : %s\n", mapping.Network)
	fmt.Printf("  Created : %s\n\n", mapping.CreatedAt)
	printDIDTable(mapping)
}

// printDIDTable renders the DID mapping as a formatted table.
func printDIDTable(mapping *didMapping) {
	fmt.Println("  ┌─────┬─────────────┬──────────┬─────────┬──────────────────────────────────────────┐")
	fmt.Println("  │  #  │ Node        │ HTTP     │ Quorum? │ Primary DID                              │")
	fmt.Println("  ├─────┼─────────────┼──────────┼─────────┼──────────────────────────────────────────┤")
	for i, n := range mapping.Nodes {
		isQ := "No"
		if n.IsQuorum {
			isQ = "Yes"
		}
		did := "-"
		if n.QuorumDID != "" {
			did = n.QuorumDID
		} else if len(n.DIDs) > 0 {
			did = n.DIDs[0]
		}
		fmt.Printf("  │ %-3d │ %-11s │ %-8d │ %-7s │ %-40s │\n",
			i+1, n.Name, n.HTTPPort, isQ, truncate(did, 40))
	}
	fmt.Println("  └─────┴─────────────┴──────────┴─────────┴──────────────────────────────────────────┘")
}

// ─────────────────────────────────────────────
// loadDIDMapping reads and parses did_mapping.json.
// Reuses the didMapping struct defined in did.go.
// ─────────────────────────────────────────────
func loadDIDMapping(path string) (*didMapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m didMapping
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
