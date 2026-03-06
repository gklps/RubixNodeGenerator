package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// basePort is rubixgoplatform's internal constant (NodePort in core.go).
	// The actual HTTP port a node binds to is: basePort + nodeIndex
	// where nodeIndex is the value passed via the -n flag.
	basePort = 20000

	// baseGrpcPort is the starting gRPC port. Unlike HTTP ports, -grpcPort
	// accepts a direct port number, so port gap applies here.
	baseGrpcPort = 10500
)

// nodeInfo holds the configuration for a single node instance.
type nodeInfo struct {
	Name      string // e.g. "quorum1", "node1"
	Path      string // relative node data directory, e.g. "Quorums/Qnode1"
	NodeIndex int    // value passed as -n flag (sequential: 0, 1, 2 ...)
	Port      int    // actual HTTP port rubixgoplatform listens on = basePort + NodeIndex
	GrpcPort  int    // direct gRPC port passed via -grpcPort
	IsQuorum  bool
}

// ─────────────────────────────────────────────
// setupNodes orchestrates Steps 5 & 6:
//   - Prompts for network, quorum count, normal count, port gap
//   - Plans node layout (paths + ports)
//   - Checks port availability
//   - Launches all nodes in background
//
// Returns the full node list so callers (e.g. DID setup) can use it.
// ─────────────────────────────────────────────
func setupNodes(installPath string, cfg *Config) ([]nodeInfo, error) {
	// ── Step 5a: Network selection ────────────────────────────────────────────
	cfg.TestNet = promptNetwork(cfg)

	// ── Step 5b: Node counts ─────────────────────────────────────────────────
	cfg.QuorumNodes = promptQuorumNodes(cfg)
	cfg.NormalNodes = promptNormalNodes(cfg)

	// ── Step 5c: Port gap ────────────────────────────────────────────────────
	cfg.PortGap = promptPortGap(cfg)

	// ── Build node plan ───────────────────────────────────────────────────────
	nodes := buildNodePlan(cfg)

	// ── Display plan and confirm ──────────────────────────────────────────────
	printNodePlan(nodes)
	if !readYesNo("\nProceed to launch all nodes? [Y/n]: ") {
		printWarn("Node launch cancelled by user.")
		return nil, nil
	}

	// ── Create directories + check ports + launch ─────────────────────────────
	for _, n := range nodes {
		nodePath := filepath.Join(installPath, filepath.FromSlash(n.Path))
		if err := ensureDir(nodePath); err != nil {
			return nil, fmt.Errorf("create node dir %s: %w", nodePath, err)
		}

		// Port availability check
		if !isPortAvailable(n.Port) {
			return nil, fmt.Errorf("port %d (node %s) is already in use. "+
				"Stop the conflicting process or choose a different port gap.", n.Port, n.Name)
		}
		if !isPortAvailable(n.GrpcPort) {
			return nil, fmt.Errorf("grpc port %d (node %s) is already in use. "+
				"Stop the conflicting process or choose a different port gap.", n.GrpcPort, n.Name)
		}

		printProgress(fmt.Sprintf("Launching %s (port=%d grpcPort=%d) ...", n.Name, n.Port, n.GrpcPort))

		if err := launchNode(installPath, n, cfg.TestNet); err != nil {
			return nil, fmt.Errorf("launch node %s: %w", n.Name, err)
		}

		printSuccess(fmt.Sprintf("Node %s started. Log: %s.txt", n.Name, n.Name))
	}

	return nodes, nil
}

// ─────────────────────────────────────────────
// Prompts
// ─────────────────────────────────────────────

func promptNetwork(cfg *Config) bool {
	if cfg.TestNet { // already set via --testnet flag
		printProgress("Network: Testnet (from flag)")
		return true
	}
	fmt.Println("\nSelect network:")
	fmt.Println("  0 - Mainnet  (production, real RBT tokens)")
	fmt.Println("  1 - Testnet  (development / testing)")
	choice := readInt("\nEnter selection [0/1]: ", 0, 1)
	if choice == 1 {
		printSuccess("Network: Testnet")
		return true
	}
	printSuccess("Network: Mainnet")
	return false
}

func promptQuorumNodes(cfg *Config) int {
	if cfg.QuorumNodes > 0 {
		printProgress(fmt.Sprintf("Quorum nodes from flag: %d", cfg.QuorumNodes))
		return cfg.QuorumNodes
	}
	fmt.Println("\nQuorum nodes validate transactions on the network.")
	return readIntDefault(
		"Enter number of Quorum nodes (Press Enter for default 5): ",
		1, 100, 5,
	)
}

func promptNormalNodes(cfg *Config) int {
	if cfg.NormalNodes > 0 {
		printProgress(fmt.Sprintf("Normal nodes from flag: %d", cfg.NormalNodes))
		return cfg.NormalNodes
	}
	fmt.Println("\nNormal nodes participate in the network without quorum responsibilities.")
	return readIntDefault(
		"Enter number of Normal nodes (Press Enter for default 5): ",
		0, 100, 5,
	)
}

func promptPortGap(cfg *Config) int {
	if cfg.PortGap > 0 {
		printProgress(fmt.Sprintf("Port gap from flag: %d", cfg.PortGap))
		return cfg.PortGap
	}
	fmt.Println("\nPort gap is the spacing between consecutive node ports.")
	fmt.Printf("  Example: gap=10 → ports %d, %d, %d ...\n",
		basePort, basePort+10, basePort+20)
	return readIntDefault(
		"Enter port gap (Press Enter for default 10): ",
		1, 1000, 10,
	)
}

// ─────────────────────────────────────────────
// buildNodePlan creates the full node list with ports assigned.
//
// Key insight from rubixgoplatform source (core/core.go):
//   -n flag is a NODE INDEX, not a port number.
//   Actual HTTP port = 20000 (NodePort constant) + nodeIndex
//
// Therefore:
//   NodeIndex = globalIndex (0, 1, 2, ...)  → passed as -n
//   Port      = basePort + globalIndex       → actual HTTP port the node binds to
//   GrpcPort  = baseGrpcPort + (globalIndex * portGap) → direct port via -grpcPort
//
// ─────────────────────────────────────────────
func buildNodePlan(cfg *Config) []nodeInfo {
	nodes := make([]nodeInfo, 0, cfg.QuorumNodes+cfg.NormalNodes)
	globalIndex := 0

	// Quorum nodes
	for i := 1; i <= cfg.QuorumNodes; i++ {
		offset := globalIndex * cfg.PortGap
		nodes = append(nodes, nodeInfo{
			Name:      fmt.Sprintf("quorum%d", i),
			Path:      fmt.Sprintf("Quorums/Qnode%d", i),
			NodeIndex: offset,              // passed as -n; rubixgoplatform computes HTTP = 20000 + offset
			Port:      basePort + offset,   // actual HTTP port
			GrpcPort:  baseGrpcPort + offset,
			IsQuorum:  true,
		})
		globalIndex++
	}

	// Normal nodes
	for i := 1; i <= cfg.NormalNodes; i++ {
		offset := globalIndex * cfg.PortGap
		nodes = append(nodes, nodeInfo{
			Name:      fmt.Sprintf("node%d", i),
			Path:      fmt.Sprintf("Node/node%d", i),
			NodeIndex: offset,
			Port:      basePort + offset,
			GrpcPort:  baseGrpcPort + offset,
			IsQuorum:  false,
		})
		globalIndex++
	}

	return nodes
}

// printNodePlan displays the planned node layout as a table.
func printNodePlan(nodes []nodeInfo) {
	fmt.Println("\n┌─────────────┬──────────────────────────┬──────┬──────────┬──────────┐")
	fmt.Println("│ Node        │ Path                     │ -n   │ HTTP     │ gRPC     │")
	fmt.Println("│             │                          │ idx  │ port     │ port     │")
	fmt.Println("├─────────────┼──────────────────────────┼──────┼──────────┼──────────┤")
	for _, n := range nodes {
		fmt.Printf("│ %-11s │ %-24s │ %-4d │ %-8d │ %-8d │\n",
			n.Name, n.Path, n.NodeIndex, n.Port, n.GrpcPort)
	}
	fmt.Println("└─────────────┴──────────────────────────┴──────┴──────────┴──────────┘")
	fmt.Println("  HTTP port = 20000 + node index (computed by rubixgoplatform internally)")
}

// ─────────────────────────────────────────────
// launchNode starts a single node in the background.
//
// Linux/macOS: tries screen first, falls back to nohup.
// Windows:     uses PowerShell Start-Process.
// ─────────────────────────────────────────────
func launchNode(installPath string, n nodeInfo, testNet bool) error {
	// Build the rubixgoplatform run command arguments
	runArgs := buildRunArgs(n, testNet)

	logFile := n.Name + ".txt"

	switch runtime.GOOS {
	case "windows":
		return launchWindows(installPath, n.Name, runArgs, logFile)
	default: // linux, darwin
		return launchUnix(installPath, n.Name, runArgs, logFile)
	}
}

// buildRunArgs returns the arguments for `rubixgoplatform run ...`
// -n receives NodeIndex (0, 1, 2...) — rubixgoplatform computes HTTP port as 20000+n internally.
// -grpcPort receives the direct gRPC port number.
func buildRunArgs(n nodeInfo, testNet bool) []string {
	args := []string{
		"run",
		"-p", n.Path,
		"-n", fmt.Sprintf("%d", n.NodeIndex),
		"-s",
		"-grpcPort", fmt.Sprintf("%d", n.GrpcPort),
	}
	if testNet {
		args = append(args, "-testNet")
	}
	args = append(args, "-enableTrustedNetwork")
	return args
}

// ─────────────────────────────────────────────
// launchUnix uses `screen` if available, otherwise `nohup`.
// ─────────────────────────────────────────────
func launchUnix(installPath, sessionName string, runArgs []string, logFile string) error {
	binary := filepath.Join(installPath, "rubixgoplatform")
	cmdStr := shellQuoteArgs(binary, runArgs)
	fullCmd := fmt.Sprintf("%s | tee %s", cmdStr, logFile)

	// Try screen first
	if screenPath, err := exec.LookPath("screen"); err == nil {
		args := []string{
			"-dmS", sessionName,
			"bash", "-c", fullCmd,
		}
		cmd := exec.Command(screenPath, args...)
		cmd.Dir = installPath
		if err := cmd.Run(); err != nil {
			printWarn(fmt.Sprintf("screen failed (%v); falling back to nohup.", err))
		} else {
			return nil
		}
	} else {
		printWarn(fmt.Sprintf("screen not found (%v); using nohup.", err))
	}

	// Fallback: nohup
	nohupCmd := fmt.Sprintf("nohup %s > %s 2>&1 &", cmdStr, logFile)
	cmd := exec.Command("bash", "-c", nohupCmd)
	cmd.Dir = installPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ─────────────────────────────────────────────
// launchWindows uses PowerShell Start-Process to launch the node
// in a hidden background window with output redirected to a log file.
// ─────────────────────────────────────────────
func launchWindows(installPath, sessionName string, runArgs []string, logFile string) error {
	binary := filepath.Join(installPath, "rubixgoplatform.exe")

	// Build the argument string for Start-Process
	// e.g. "run -p Node\node1 -n 20000 -s -grpcPort 10500 -enableTrustedNetwork"
	argStr := strings.Join(runArgs, " ")

	// PowerShell command:
	//   Start-Process -FilePath ".\rubixgoplatform.exe"
	//                 -ArgumentList "run -p ... "
	//                 -RedirectStandardOutput "node1.txt"
	//                 -WindowStyle Hidden
	psCmd := fmt.Sprintf(
		`Start-Process -FilePath "%s" -ArgumentList "%s" -RedirectStandardOutput "%s" -WindowStyle Hidden -WorkingDirectory "%s"`,
		binary,
		argStr,
		filepath.Join(installPath, logFile),
		installPath,
	)

	_ = sessionName // used as a label; Windows doesn't have named sessions

	cmd := exec.Command("powershell", "-NoProfile", "-Command", psCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// shellQuoteArgs returns a shell-safe command string from binary + args.
func shellQuoteArgs(binary string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, fmt.Sprintf("%q", binary))
	for _, a := range args {
		parts = append(parts, fmt.Sprintf("%q", a))
	}
	return strings.Join(parts, " ")
}
