# Port Mapping Reference — rubixgoplatform

> Source: `rubixgoplatform/core/core.go`
> Last verified: 2026-02-19 against branch `development`

---

## Constants Defined in rubixgoplatform

```go
const (
    NodePort    uint16 = 20000   // HTTP server base port
    SendPort    uint16 = 21000   // Send port base
    RecvPort    uint16 = 22000   // Receiver port base
    IPFSPort    uint16 = 5002    // IPFS API port base
    SwarmPort   uint16 = 4002    // IPFS Swarm port base
    IPFSAPIPort uint16 = 8081    // IPFS Gateway port base
    MaxPeerConn uint16 = 1000    // Multiplier for receiver port spacing
)
```

---

## Port Formulas (per node)

The `-n` flag passed to `rubixgoplatform run` is the **node index** (what rubix-setup passes as `NodeIndex = globalIndex * portGap`).

| Port Type     | Formula                          | CLI Flag      |
|---------------|----------------------------------|---------------|
| HTTP          | `20000 + nodeIndex`              | computed from `-n` |
| gRPC          | direct value passed              | `-grpcPort`   |
| Send          | `21000 + nodeIndex`              | internal      |
| Receiver      | `22000 + (1000 × nodeIndex)`     | internal      |
| IPFS API      | `5002  + nodeIndex`              | internal      |
| IPFS Swarm    | `4002  + nodeIndex`              | internal      |
| IPFS Gateway  | `8081  + nodeIndex`              | internal      |

> **Warning:** With large `nodeIndex` values, the Receiver port can exceed 65535.
> Formula: `22000 + 1000 × nodeIndex ≤ 65535` → `nodeIndex ≤ 43`
> For safety, keep `portGap × totalNodes ≤ 43`.

---

## Example: Default Setup (portGap = 10, 6 Quorum + 3 Normal)

| Node    | NodeIndex (-n) | HTTP  | gRPC  | Send  | Receiver | IPFS API | IPFS Swarm |
|---------|----------------|-------|-------|-------|----------|----------|------------|
| quorum1 | 0              | 20000 | 10500 | 21000 | 22000    | 5002     | 4002       |
| quorum2 | 10             | 20010 | 10510 | 21010 | 32000    | 5012     | 4012       |
| quorum3 | 20             | 20020 | 10520 | 21020 | 42000    | 5022     | 4022       |
| quorum4 | 30             | 20030 | 10530 | 21030 | 52000    | 5032     | 4032       |
| quorum5 | 40             | 20040 | 10540 | 21040 | 62000    | 5042     | 4042       |
| quorum6 | 50             | 20050 | 10550 | 21050 | 72000    | 5052     | 4052       |
| node1   | 60             | 20060 | 10560 | 21060 | 82000    | 5062     | 4062       |
| node2   | 70             | 20070 | 10570 | 21070 | 92000    | 5072     | 4072       |
| node3   | 80             | 20080 | 10580 | 21080 | 102000   | 5082     | 4082       |

---

## Example: portGap = 197 (6 Quorum + 3 Normal)

| Node    | NodeIndex (-n) | HTTP  | gRPC  | Send  | Receiver  | IPFS API | IPFS Swarm |
|---------|----------------|-------|-------|-------|-----------|----------|------------|
| quorum1 | 0              | 20000 | 10500 | 21000 | 22000     | 5002     | 4002       |
| quorum2 | 197            | 20197 | 10697 | 21197 | 219000 ⚠️ | 5199     | 4199       |
| quorum3 | 394            | 20394 | 10894 | 21394 | 416000 ⚠️ | 5396     | 4396       |

> ⚠️ Receiver port exceeds 65535 for nodeIndex > 43. rubixgoplatform may handle this
> internally (port wrapping or error) — test before production use with large gaps.

---

## How rubix-setup Calculates Ports

```go
// nodes.go — buildNodePlan()
for i, node := range allNodes {
    offset := globalIndex * cfg.PortGap   // e.g. 0, 10, 20, 30...

    nodeInfo{
        NodeIndex: offset,                // passed as: -n <offset>
        Port:      20000 + offset,        // actual HTTP port rubixgoplatform binds to
        GrpcPort:  10500 + offset,        // passed as: -grpcPort <grpcPort>
    }
    globalIndex++
}
```

The `-grpcPort` is a **direct port number** (not an index), so it binds exactly to the value passed.

---

## Command Reference

```bash
# Check what HTTP port a node is on (from lsof)
lsof -iTCP -sTCP:LISTEN -nP | grep rubixgop | awk '{print $9}' | sort -t: -k2 -n

# Interact with a specific node
./rubixgoplatform getalldid          -port <HTTP_PORT>
./rubixgoplatform createdid          -didType 4 -port <HTTP_PORT>
./rubixgoplatform setupquorum        -did <DID> -port <HTTP_PORT>
./rubixgoplatform generatetestrbt    -did <DID> -port <HTTP_PORT>
./rubixgoplatform initiatedocontract -port <HTTP_PORT>

# Node run command (how rubix-setup launches each node)
./rubixgoplatform run \
    -p <nodePath> \
    -n <nodeIndex> \
    -s \
    -grpcPort <grpcPort> \
    [-testNet] \
    -enableTrustedNetwork
```

---

## Port Availability Check (rubix-setup internal)

Before launching each node, rubix-setup verifies both the HTTP and gRPC ports are free:

```go
// utils.go — isPortAvailable()
ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
// if err != nil → port is in use → abort with clear error message
```

---

## Verified Port Bindings (from lsof on live testnet run)

Observed on macOS arm64, Darwin 25.2.0, with portGap=10, 6Q+3N:

```
127.0.0.1:10500   gRPC  quorum1
127.0.0.1:10510   gRPC  quorum2
127.0.0.1:10520   gRPC  quorum3
127.0.0.1:10530   gRPC  quorum4
127.0.0.1:10540   gRPC  quorum5
127.0.0.1:10550   gRPC  quorum6
127.0.0.1:10560   gRPC  node1
127.0.0.1:10570   gRPC  node2
127.0.0.1:10580   gRPC  node3
127.0.0.1:20000   HTTP  quorum1
127.0.0.1:20001   HTTP  quorum2   ← Note: gap=1 when portGap=10 because
127.0.0.1:20002   HTTP  quorum3      nodeIndex was not multiplied by portGap
...                                  (BUG - now FIXED in nodes.go)
```

**After fix** (nodeIndex = globalIndex × portGap):
```
127.0.0.1:20000   HTTP  quorum1  (nodeIndex=0)
127.0.0.1:20010   HTTP  quorum2  (nodeIndex=10)
127.0.0.1:20020   HTTP  quorum3  (nodeIndex=20)
...
```
