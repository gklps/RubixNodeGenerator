package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ─────────────────────────────────────────────
// DID mapping output written to did_mapping.json
// ─────────────────────────────────────────────

type nodeDIDRecord struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	HTTPPort   int      `json:"http_port"`
	GrpcPort   int      `json:"grpc_port"`
	NodeIndex  int      `json:"node_index"`
	IsQuorum   bool     `json:"is_quorum"`
	DIDs       []string `json:"dids"`
	QuorumDID  string   `json:"quorum_did,omitempty"`
	QuorumDone bool     `json:"quorum_setup"`
}

type didMapping struct {
	Network   string          `json:"network"`
	CreatedAt string          `json:"created_at"`
	Nodes     []nodeDIDRecord `json:"nodes"`
}

// ─────────────────────────────────────────────
// setupDIDs is Step 6.5: creates DIDs on every node and enables quorum
// on quorum nodes.
//
// Flow:
//  1. Wait for nodes to stabilise (60 s, skippable)
//  2. Ask how many DIDs to create per node
//  3. For each node: create DIDs, then run setupquorum if IsQuorum
//  4. Save did_mapping.json
//
// ─────────────────────────────────────────────
func setupDIDs(installPath string, nodes []nodeInfo, cfg *Config) error {
	if len(nodes) == 0 {
		printWarn("No nodes available for DID setup — skipping.")
		return nil
	}

	// ── 6.5a: Wait for nodes to boot ─────────────────────────────────────────
	promptWaitForNodes()

	// ── 6.5b: Ask DID count ───────────────────────────────────────────────────
	didCount := promptDIDCount()

	// ── 6.5c: Per-node DID creation + quorum setup ───────────────────────────
	records := make([]nodeDIDRecord, 0, len(nodes))

	for _, n := range nodes {
		printProgress(fmt.Sprintf("Setting up DIDs on %s (port=%d) ...", n.Name, n.Port))

		rec := nodeDIDRecord{
			Name:      n.Name,
			Path:      n.Path,
			HTTPPort:  n.Port,
			GrpcPort:  n.GrpcPort,
			NodeIndex: n.NodeIndex,
			IsQuorum:  n.IsQuorum,
			DIDs:      []string{},
		}

		// Create the requested number of DIDs
		for i := 0; i < didCount; i++ {
			did, err := createDID(installPath, n.Port)
			if err != nil {
				printWarn(fmt.Sprintf("  DID %d creation failed on %s: %v", i+1, n.Name, err))
				continue
			}
			rec.DIDs = append(rec.DIDs, did)
			printSuccess(fmt.Sprintf("  DID created: %s", did))
		}

		// Quorum setup: use the first DID
		if n.IsQuorum && len(rec.DIDs) > 0 {
			primaryDID := rec.DIDs[0]
			printProgress(fmt.Sprintf("  Enabling quorum on %s (DID=%s) ...", n.Name, primaryDID))
			if err := setupQuorum(installPath, primaryDID, n.Port); err != nil {
				printWarn(fmt.Sprintf("  Quorum setup failed on %s: %v", n.Name, err))
			} else {
				rec.QuorumDID = primaryDID
				rec.QuorumDone = true
				printSuccess(fmt.Sprintf("  Quorum enabled on %s", n.Name))
			}
		}

		records = append(records, rec)
	}

	// ── 6.5d: Save did_mapping.json ───────────────────────────────────────────
	network := "mainnet"
	if cfg.TestNet {
		network = "testnet"
	}
	mapping := didMapping{
		Network:   network,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Nodes:     records,
	}
	if err := saveDIDMapping(installPath, mapping); err != nil {
		printWarn(fmt.Sprintf("Could not write did_mapping.json: %v", err))
	}

	return nil
}

// ─────────────────────────────────────────────
// promptWaitForNodes asks the user to wait for nodes to boot.
// Default wait is 60 seconds with a live countdown; user can skip.
// ─────────────────────────────────────────────
func promptWaitForNodes() {
	fmt.Println()
	fmt.Println("Nodes are starting up. They need ~60 seconds to initialise IPFS and open ports.")
	fmt.Println("  W - Wait 60 seconds (recommended)")
	fmt.Println("  S - Skip (proceed immediately — DID calls may fail if nodes aren't ready)")
	fmt.Println()

	choice := strings.ToUpper(strings.TrimSpace(readLine("Your choice [W/s]: ")))
	if choice == "S" {
		printWarn("Skipping wait. Proceed at your own risk.")
		return
	}

	// Countdown
	total := 60
	for i := total; i > 0; i-- {
		fmt.Printf("\r  Waiting %2d seconds... (Ctrl+C to abort)   ", i)
		time.Sleep(time.Second)
	}
	fmt.Println("\r  Wait complete.                              ")
	printSuccess("Nodes should be ready. Proceeding with DID setup.")
}

// ─────────────────────────────────────────────
// promptDIDCount asks how many DIDs to create per node.
// ─────────────────────────────────────────────
func promptDIDCount() int {
	fmt.Println()
	fmt.Println("How many DIDs do you want to create per node?")
	fmt.Println("  (1 DID is sufficient for most setups.)")
	return readIntDefault("Enter DID count per node (Press Enter for default 1): ", 1, 10, 1)
}

// ─────────────────────────────────────────────
// getAllDIDs runs `rubixgoplatform getalldid` and returns the DID strings.
// Output format (each line): "  DID <did_string>"
// ─────────────────────────────────────────────
func getAllDIDs(installPath string, port int) ([]string, error) {
	out, err := runRubixCmd(installPath, "getalldid", "-port", fmt.Sprintf("%d", port))
	if err != nil {
		return nil, err
	}

	var dids []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		// The getalldid output contains lines like: "DID bafybmi..."
		// Accept lines that look like DID strings (start with "baf" or contain DID marker)
		if strings.HasPrefix(line, "baf") {
			dids = append(dids, line)
			continue
		}
		// Some versions prefix with "DID " or show as "did :"
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "did ") || strings.HasPrefix(lower, "did:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				dids = append(dids, parts[len(parts)-1])
			}
		}
	}
	return dids, nil
}

// ─────────────────────────────────────────────
// createDID runs `rubixgoplatform createdid -didType 4` and returns the DID string.
//
// Expected output contains a line like:
//
//	"DID bafybmi... Created successfully"
//
// ─────────────────────────────────────────────
func createDID(installPath string, port int) (string, error) {
	out, err := runRubixCmd(installPath, "createdid",
		"-didType", "4",
		"-port", fmt.Sprintf("%d", port),
	)
	if err != nil {
		return "", err
	}

	// Parse the DID from output
	did := parseDIDFromOutput(out)
	if did == "" {
		return "", fmt.Errorf("DID not found in output: %s", strings.TrimSpace(out))
	}
	return did, nil
}

// parseDIDFromOutput scans rubixgoplatform output for a DID string.
// DID strings start with "baf" (CID v1 multibase base32).
func parseDIDFromOutput(out string) string {
	for _, line := range strings.Split(out, "\n") {
		for _, word := range strings.Fields(line) {
			if strings.HasPrefix(word, "baf") && len(word) > 20 {
				return word
			}
		}
	}
	return ""
}

// ─────────────────────────────────────────────
// setupQuorum runs `rubixgoplatform setupquorum -did <did> -port <port>`
// ─────────────────────────────────────────────
func setupQuorum(installPath string, did string, port int) error {
	out, err := runRubixCmd(installPath, "setupquorum",
		"-did", did,
		"-port", fmt.Sprintf("%d", port),
	)
	if err != nil {
		return fmt.Errorf("%w\nOutput: %s", err, strings.TrimSpace(out))
	}

	// Check for success indicator in output
	lower := strings.ToLower(out)
	if strings.Contains(lower, "error") && !strings.Contains(lower, "success") {
		return fmt.Errorf("setupquorum reported error: %s", strings.TrimSpace(out))
	}
	return nil
}

// ─────────────────────────────────────────────
// saveDIDMapping writes did_mapping.json to the install directory.
// ─────────────────────────────────────────────
func saveDIDMapping(installPath string, mapping didMapping) error {
	b, err := json.MarshalIndent(mapping, "", "  ")
	if err != nil {
		return err
	}

	dst := filepath.Join(installPath, "did_mapping.json")
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return err
	}
	printSuccess(fmt.Sprintf("did_mapping.json written to %s", dst))
	return nil
}

// ─────────────────────────────────────────────
// runRubixCmd executes `rubixgoplatform <subcommand> [args...]`
// from the install directory and returns combined stdout+stderr as a string.
// ─────────────────────────────────────────────
func runRubixCmd(installPath string, args ...string) (string, error) {
	binary := filepath.Join(installPath, executableName("rubixgoplatform"))
	cmd := exec.Command(binary, args...)
	cmd.Dir = installPath
	out, err := cmd.CombinedOutput()
	return string(out), err
}
