# Session Manager Architecture

> Source: `session.go`, `shutdown.go`, `nodes.go`, `did.go`
> Last updated: 2026-02-19

---

## Overview

The session manager allows `rubix-setup` to resume control over an already-installed Rubix environment without re-running the full setup (clone, build, IPFS download). It provides an interactive command menu to manage running nodes.

**Entry point:** `resumeSession()` in `session.go`

---

## Session Detection

A directory is a valid rubix-setup session if it contains `config.json`.

```go
// session.go
func isExistingSession(path string) bool {
    return fileExists(filepath.Join(path, "config.json"))
}
```

**When detected automatically:**
```
./rubix-setup --install-path ./test
```
If `./test/config.json` exists, rubix-setup offers:
```
  1 - Resume this session  (manage running nodes)
  2 - New session          (clone, build, launch fresh)
```

**When prompted at startup (no --install-path given):**
```
  1 - New session    (clone, build, download IPFS, launch nodes)
  2 - Resume session (manage nodes in an existing install path)
```

---

## State Files

| File               | Location          | Written by      | Read by                         |
|--------------------|-------------------|-----------------|---------------------------------|
| `config.json`      | `<install_path>/` | `writeConfig()` | `loadSavedConfig()`, `isExistingSession()` |
| `did_mapping.json` | `<install_path>/` | `saveDIDMapping()` | `loadDIDMapping()`           |

### `config.json` schema

```json
{
  "branch":       "development",
  "install_path": "/Users/gokul/.../test",
  "network":      "testnet",
  "quorum_nodes": 6,
  "normal_nodes": 3,
  "base_port":    20000,
  "base_grpc":    10500,
  "port_gap":     10,
  "platform":     "darwin/arm64",
  "ipfs_version": "v0.19.1"
}
```

### `did_mapping.json` schema

```json
{
  "network":    "testnet",
  "created_at": "2026-02-19T12:00:00Z",
  "nodes": [
    {
      "name":        "quorum1",
      "path":        "Quorums/Qnode1",
      "http_port":   20000,
      "grpc_port":   10500,
      "node_index":  0,
      "is_quorum":   true,
      "dids":        ["bafybmic3ld56..."],
      "quorum_did":  "bafybmic3ld56...",
      "quorum_setup": true
    },
    {
      "name":        "node1",
      "path":        "Node/node1",
      "http_port":   20060,
      "grpc_port":   10560,
      "node_index":  60,
      "is_quorum":   false,
      "dids":        ["bafybmid6e..."],
      "quorum_did":  "",
      "quorum_setup": false
    }
  ]
}
```

---

## Resume Flow

```
./rubix-setup --install-path ./test
      │
      ▼
promptSessionMode()          ← detects existing session
      │
      ▼  (user picks Resume)
resumeSession()
      │
      ├─ promptResumePath()       ← uses --install-path or prompts
      ├─ findConfigJSON()         ← locates config.json
      ├─ loadSavedConfig()        ← parses config.json → savedConfig{}
      ├─ loadDIDMapping()         ← parses did_mapping.json → didMapping{}
      ├─ printSessionHeader()     ← summary box
      └─ runCommandMenu()         ← interactive loop
```

---

## Key Types

### `savedConfig` (shutdown.go)

Used to reconstruct the node plan from `config.json` without running setup again.

```go
type savedConfig struct {
    Branch      string `json:"branch"`
    InstallPath string `json:"install_path"`
    Network     string `json:"network"`      // "testnet" or "mainnet"
    QuorumNodes int    `json:"quorum_nodes"`
    NormalNodes int    `json:"normal_nodes"`
    BasePort    int    `json:"base_port"`    // defaults to 20000
    BaseGrpc    int    `json:"base_grpc"`    // defaults to 10500
    PortGap     int    `json:"port_gap"`     // defaults to 10
}
```

### `didMapping` / `nodeDIDRecord` (did.go)

```go
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
```

### `nodeDidPair` (session.go)

A resolved (node, DID) pair ready for command execution.

```go
type nodeDidPair struct {
    Node nodeDIDRecord
    DID  string
}
```

---

## Command Menu

The `runCommandMenu()` loop in `session.go`:

```
──────────────────────── Commands ─────────────────────────
  1 - Generate test RBT tokens   (generatetestrbt)
  2 - Get account info / balance (getaccountinfo)
  3 - Setup quorum               (setupquorum)
  4 - Start offline nodes        (relaunch stopped nodes)
  5 - Show node status           (ports, screen sessions)
  6 - Show DID mapping           (from did_mapping.json)
  7 - Shutdown all nodes         (graceful shutdown)
  0 - Exit
────────────────────────────────────────────────────────────
```

---

## `promptNodeAndDID()` — Unified Node+DID Selection

All commands that target a specific node+DID use this shared helper.

**Two-level selection:**

```
  a - All DIDs on all nodes  (no further prompts)
  b - Choose specific nodes/DIDs
```

**Option `a` — Bulk mode:**
- Iterates all nodes in the mapping
- Collects every DID on every node
- Returns all pairs immediately — zero further prompts
- Skips nodes with no DIDs (prints warning)

**Option `b` — Specific mode:**
1. Shows the DID mapping table
2. Prompts: enter node number (1–N) or 0 for all nodes
3. For each selected node:
   - If node has 1 DID → auto-selected, no prompt
   - If node has multiple DIDs → shows sub-list, user picks one or `0` for all DIDs on that node

**Return value:** `[]nodeDidPair` — caller iterates and runs the command on each pair.

### Per-command DID scope

| Command           | DID scope restriction          |
|-------------------|-------------------------------|
| generatetestrbt   | Any node, any DID             |
| getaccountinfo    | Any node, any DID             |
| setupquorum       | **Quorum nodes only** (filtered before promptNodeAndDID) |
| cmdStartNodes     | No DID selection (uses saved config)  |
| cmdShowStatus     | No DID selection              |
| cmdShowDIDMapping | No DID selection              |
| Shutdown all      | No DID selection              |

For `setupquorum`, a filtered `didMapping` containing only quorum nodes is passed to `promptNodeAndDID()`:

```go
// session.go — cmdSetupQuorum()
quorumNodes := filter(mapping.Nodes, func(n nodeDIDRecord) bool { return n.IsQuorum })
quorumMapping := &didMapping{..., Nodes: quorumNodes}
pairs := promptNodeAndDID(quorumMapping)
```

---

## Command Implementations

### 1. Generate Test RBT (`cmdGenerateTestRBT`)

```go
// Flag: -numTokens (NOT -numToken — the 's' is required, verified 2026-02-19)
runRubixCmd(installPath,
    "generatetestrbt",
    "-numTokens", fmt.Sprintf("%d", numTokens),
    "-did", p.DID,
    "-port", fmt.Sprintf("%d", p.Node.HTTPPort),
)
```

Prompts for token count (default 1, range 1–10000), then runs once per `nodeDidPair`.

### 2. Get Account Info (`cmdGetAccountInfo`)

Two sub-modes:
- `a` — Single: calls `promptNodeAndDID()` then `printAccountInfo()` per pair
- `b` — All: queries every DID on every node, renders a summary table

Output parser (`parseAccountInfo`):
```
Expected line: "RBT :     15.000, Locked RBT :      0.000, Pledged RBT :      0.000, Pinned RBT :      0.000"
Split on "," → split each part on ":" → key/value map
```

### 3. Setup Quorum (`cmdSetupQuorum`)

Runs `setupquorum -did <did> -port <port>` for each selected quorum-node pair.
Only quorum nodes are shown in selection (pre-filtered).

### 4. Start Offline Nodes (`cmdStartNodes`)

**Detection logic — `isPortAvailable(port int) bool`:**
```go
// utils.go
ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
// err != nil  → port occupied → node IS running
// err == nil  → port free    → node is OFFLINE
```

For each expected node (from `buildNodePlan(savedConfig)`):
- Port NOT available → `already running` message, skip
- Port available → call `launchNode(installPath, n, testNet)`

Reports: `X already running | Y started | Z failed`

### 5. Show Node Status (`cmdShowStatus`)

- Calls `screenSessionStatus(name)` per node — checks `screen -ls` output
- Runs `lsof -iTCP -sTCP:LISTEN -nP | grep rubixgop` to show bound ports
- Table: Node | HTTP | gRPC | Screen

### 6. Show DID Mapping (`cmdShowDIDMapping`)

Prints `did_mapping.json` as a formatted table via `printDIDTable()`.

### 7. Shutdown All Nodes (`runKillAllNodes`)

Defined in `shutdown.go`. Sequence:
1. Load `config.json` → `savedConfig`
2. Rebuild node plan via `buildNodePlan()`
3. Per node: `rubixgoplatform shutdown -port <HTTP_PORT>`
4. Wait 2 seconds
5. Per node: `screen -S <name> -X quit`
6. Fallback if config not found: `pkill -f rubixgoplatform` + kill all screen sessions matching `quorum*`/`node*`

---

## Node Launch (`launchNode`)

Shared between initial setup (`setupNodes`) and resume mode (`cmdStartNodes`).

**Unix (Linux/macOS):**
1. Try `screen -dmS <name> bash -c "<binary> run ... | tee <name>.txt"`
2. If screen not available: `nohup <binary> run ... > <name>.txt 2>&1 &`

**Windows:**
```powershell
Start-Process -FilePath "<binary>" -ArgumentList "run ..." -RedirectStandardOutput "<name>.txt" -WindowStyle Hidden
```

**Run arguments built by `buildRunArgs(n nodeInfo, testNet bool)`:**
```bash
run -p <n.Path> -n <n.NodeIndex> -s -grpcPort <n.GrpcPort> [-testNet] -enableTrustedNetwork
```

---

## Node Plan Reconstruction (`buildNodePlan`)

`savedConfig` → `Config` → `buildNodePlan()` → `[]nodeInfo`

```go
// Used in cmdStartNodes, cmdShowStatus, runKillAllNodes
fakeCfg := &Config{
    QuorumNodes:  sc.QuorumNodes,
    NormalNodes:  sc.NormalNodes,
    PortGap:      sc.PortGap,
    BasePort:     sc.BasePort,
    BaseGrpcPort: sc.BaseGrpc,
}
nodes := buildNodePlan(fakeCfg)
```

`buildNodePlan` produces deterministic node ordering: quorum nodes first, then normal nodes, numbered from 1.

---

## Adding a New Command

1. **Implement** `cmdMyCommand(installPath string, sc *savedConfig, mapping *didMapping)` in `session.go`
2. **Add menu entry** in `runCommandMenu()`:
   ```go
   fmt.Println("  8 - My new command")
   ```
3. **Add case** in the switch:
   ```go
   case 8:
       cmdMyCommand(installPath, sc, mapping)
   ```
4. **Update** `readInt("Your choice: ", 0, 8)` max bound
5. If command targets node+DID → use `promptNodeAndDID(mapping)` (or filtered mapping for scoped nodes)
6. If command runs a rubixgoplatform subcommand → use `runRubixCmd(installPath, "subcommand", ...flags)`
7. Update this doc and `CLAUDE.md` Section 5

---

## Screen Session Management

| Action                          | Command                                          |
|---------------------------------|--------------------------------------------------|
| List all sessions               | `screen -ls`                                     |
| Attach to node log              | `screen -r quorum1`                              |
| Detach (stay running)           | `Ctrl+A` then `D`                                |
| Kill session (not process)      | `screen -S quorum1 -X quit`                      |
| Kill process only               | `pkill -f rubixgoplatform`                       |
| Kill both session + process     | `screen -S quorum1 -X quit && pkill -f rubixgop` |

**Warning:** `screen -X quit` terminates the screen session but does NOT kill the child process (`rubixgoplatform run`). Always call `rubixgoplatform shutdown -port <port>` first for a clean shutdown.
