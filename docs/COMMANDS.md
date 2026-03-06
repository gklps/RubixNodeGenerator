# rubixgoplatform Command Reference

> Last verified: 2026-02-19
> Branch: `development`
> Platform tested: macOS arm64 (Darwin 25.2.0)

All commands are run from the **install directory** where the `rubixgoplatform` binary lives.

```bash
cd <install_path>
./rubixgoplatform <subcommand> [flags]
```

---

## Node Lifecycle

### `run` — Start a node

```bash
./rubixgoplatform run \
    -p <nodePath>             \   # Node data directory (relative to install dir)
    -n <nodeIndex>            \   # Node index — HTTP port = 20000 + nodeIndex
    -s                        \   # Enable server mode
    -grpcPort <grpcPort>      \   # Direct gRPC port number (e.g. 10500)
    [-testNet]                \   # Use testnet (omit for mainnet)
    -enableTrustedNetwork         # Required for local/dev setups
```

**Example (quorum1, portGap=10):**
```bash
./rubixgoplatform run -p Quorums/Qnode1 -n 0 -s -grpcPort 10500 -testNet -enableTrustedNetwork
```

**Port mapping from `-n` flag:**
| NodeIndex (`-n`) | HTTP port | Formula              |
|------------------|-----------|----------------------|
| 0                | 20000     | 20000 + 0            |
| 10               | 20010     | 20000 + 10           |
| 20               | 20020     | 20000 + 20           |

> **Important:** `-n` is NOT the HTTP port. rubixgoplatform adds 20000 internally.
> rubix-setup computes `nodeIndex = globalIndex * portGap` to create a spacing.

### `shutdown` — Gracefully stop a node

```bash
./rubixgoplatform shutdown -port <HTTP_PORT>
```

**Examples:**
```bash
./rubixgoplatform shutdown -port 20000   # stop quorum1
./rubixgoplatform shutdown -port 20010   # stop quorum2
./rubixgoplatform shutdown -port 20060   # stop node1 (portGap=10, 6Q+3N)
```

Used by `rubix-setup --killAllNode` to shut down all configured nodes.

---

## DID Management

### `createdid` — Create a new DID

```bash
./rubixgoplatform createdid -didType 4 -port <HTTP_PORT>
```

- `-didType 4` — BLS key type (required for Rubix testnet/mainnet)
- Output contains a line like: `DID bafybmi... Created successfully`
- DID strings start with `baf` (CID v1 multibase base32, ~60 chars long)

**Example:**
```bash
./rubixgoplatform createdid -didType 4 -port 20000
```

rubix-setup parses the DID from output and saves it in `did_mapping.json`.

### `getalldid` — List all DIDs on a node

```bash
./rubixgoplatform getalldid -port <HTTP_PORT>
```

Output format varies by version. rubix-setup handles these patterns:
- Lines starting with `baf…` (raw DID)
- Lines starting with `DID ` or `did:` (prefixed DID)

---

## Quorum Management

### `setupquorum` — Enable a DID as a quorum validator

```bash
./rubixgoplatform setupquorum -did <DID> -port <HTTP_PORT>
```

- Must be run on a quorum node only
- The DID must have been created first via `createdid`
- Any DID on the node can be used (not restricted to the first DID)
- rubix-setup automatically runs this on quorum nodes during initial DID setup

**Example:**
```bash
./rubixgoplatform setupquorum \
    -did bafybmic3ld56xj7v3mlcmpjl6rn5n5fkrgthu5hbnmomcrnlhnhgrgzpkj \
    -port 20000
```

---

## Token Operations

### `generatetestrbt` — Generate test RBT tokens

```bash
./rubixgoplatform generatetestrbt \
    -numTokens <count>   \   # Number of tokens (NOT -numToken; the 's' is required)
    -did <DID>           \   # Target DID to receive the tokens
    -port <HTTP_PORT>        # Node's HTTP port
```

> **Critical flag name:** `-numTokens` (with `s`). The flag `-numToken` (no `s`) does NOT work.
> Verified from live run on 2026-02-19.

**Example:**
```bash
./rubixgoplatform generatetestrbt -numTokens 15 \
    -did bafybmic3ld56xj7v3mlcmpjl6rn5n5fkrgthu5hbnmomcrnlhnhgrgzpkj \
    -port 20000
```

Works on testnet only. Tokens are credited to the given DID on the specified node.

### `getaccountinfo` — Query a DID's token balance

```bash
./rubixgoplatform getaccountinfo -did <DID> -port <HTTP_PORT>
```

**Example output line:**
```
RBT :     15.000, Locked RBT :      0.000, Pledged RBT :      0.000, Pinned RBT :      0.000
```

rubix-setup parses this with `parseAccountInfo()` in `session.go`, splitting on commas then `:`.

**Balance fields:**
| Field       | Meaning                             |
|-------------|-------------------------------------|
| RBT         | Unlocked, spendable token balance   |
| Locked RBT  | Tokens locked in pending txns       |
| Pledged RBT | Tokens pledged as quorum collateral |
| Pinned RBT  | Tokens pinned by the IPFS layer     |

---

## Transaction Operations

### `initiatedocontract`

```bash
./rubixgoplatform initiatedocontract -port <HTTP_PORT>
```

Initiates a data-only (DO) smart contract. Requires the node to be fully synced with the network.

---

## IPFS / Network Diagnostics

These commands are lower-level and primarily useful for debugging:

```bash
# Check what ports are being used by rubixgoplatform
lsof -iTCP -sTCP:LISTEN -nP | grep rubixgop | awk '{print $9}' | sort -t: -k2 -n

# Check active screen sessions (nodes running via rubix-setup)
screen -ls | grep -E '\.(quorum|node)[0-9]'

# Attach to a specific node's log (to read real-time output)
screen -r quorum1
# Detach again: Ctrl+A then D
```

---

## Full Port Formula Reference

For nodes started via rubix-setup with `portGap=10`, `6 quorum + 3 normal`:

| Node    | `-n` (NodeIndex) | HTTP Port | gRPC Port | Screen Session |
|---------|-----------------|-----------|-----------|----------------|
| quorum1 | 0               | 20000     | 10500     | quorum1        |
| quorum2 | 10              | 20010     | 10510     | quorum2        |
| quorum3 | 20              | 20020     | 10520     | quorum3        |
| quorum4 | 30              | 20030     | 10530     | quorum4        |
| quorum5 | 40              | 20040     | 10540     | quorum5        |
| quorum6 | 50              | 20050     | 10550     | quorum6        |
| node1   | 60              | 20060     | 10560     | node1          |
| node2   | 70              | 20070     | 10570     | node2          |
| node3   | 80              | 20080     | 10580     | node3          |

See `docs/PORT_MAPPING.md` for the full port formula and edge-case analysis.

---

## rubix-setup CLI Reference

### New session

```bash
# Interactive (all prompts)
./rubix-setup

# Non-interactive (all defaults: main branch, 5Q+5N, portGap=10, mainnet)
./rubix-setup --auto

# Custom configuration
./rubix-setup \
    --branch development \
    --install-path ./test \
    --testnet \
    --quorum-nodes 6 \
    --normal-nodes 3 \
    --port-gap 10
```

### Resume / utility

```bash
# Resume existing session (interactive menu)
./rubix-setup --install-path ./test

# Check running nodes
./rubix-setup status

# Shutdown all nodes gracefully
./rubix-setup --killAllNode
./rubix-setup --killAllNode --install-path ./test
```

### Flag reference

| Flag             | Type   | Default           | Description                              |
|------------------|--------|-------------------|------------------------------------------|
| `--auto`         | bool   | false             | Skip all prompts, use defaults           |
| `--branch`       | string | "" (prompt)       | rubixgoplatform branch to clone          |
| `--install-path` | string | "" (prompt)       | Directory for binaries and node data     |
| `--testnet`      | bool   | false             | Use testnet instead of mainnet           |
| `--quorum-nodes` | int    | 0 (prompt→5)      | Number of quorum nodes                   |
| `--normal-nodes` | int    | 0 (prompt→5)      | Number of normal nodes                   |
| `--port-gap`     | int    | 0 (prompt→10)     | Port spacing between nodes               |
| `--killAllNode`  | bool   | false             | Gracefully shutdown all running nodes    |
