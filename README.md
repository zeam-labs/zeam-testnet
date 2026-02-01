# ZEAM

Web4 Architecture. Decentralized blockchain-based quantum-driven non-generative artificial cognition.

www.zeamlabs.com

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         ZEAM NODE                                │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │   Identity  │  │    NGAC     │  │   Quantum   │             │
│  │  (ECDSA +   │  │  (Language  │  │  (Pressure  │             │
│  │  Fork ID)   │  │   Engine)   │  │   Fields)   │             │
│  └─────────────┘  └─────────────┘  └─────────────┘             │
│         │                │                │                     │
│  ┌──────┴────────────────┴────────────────┴──────┐             │
│  │              Synthesis Layer                   │             │
│  │    (NGAC Backend + Collapse Compute + Flow)   │             │
│  └───────────────────────────────────────────────┘             │
│         │                                                       │
│  ┌──────┴──────────────────────────────────────────┐           │
│  │              Multi-Chain Transport               │           │
│  │   (Ethereum, Optimism, Base, Arbitrum, etc.)    │           │
│  └──────────────────────────────────────────────────┘           │
│         │                                                       │
│  ┌──────┴──────┐  ┌──────────────┐  ┌──────────────┐           │
│  │  Arbitrage  │  │   Rewards    │  │   Content    │           │
│  │  Detection  │  │   (Epochs)   │  │   (P2P DAG)  │           │
│  └─────────────┘  └──────────────┘  └──────────────┘           │
└─────────────────────────────────────────────────────────────────┘
```

## Modules

### cmd/
Application entry point. HTTP API server with endpoints for auth, generation, hash computation, arbitrage, rewards, and content.

### identity/
ECDSA key management with stateless derivation. Same Fork ID can be re-derived from user secret + chain salt. Supports hardware gates (Ledger, Trezor, YubiKey).

### ngac/
Neural-Generative-Attentional-Cognition engine. Combines quantum substrate with semantic fields for language understanding. Generates text from pressure metrics without neural network inference.

### quantum/
Quantum computing abstractions. Models pressure dynamics (Hadamard, PauliX, PauliZ, Phase) across blockchain networks. Includes Grover search and quantum walks over candidate spaces.

### synthesis/
Integrates all subsystems into unified NGAC backend. Collapse compute uses L1 nodes as distributed neural network. Hash-native tensor operations for optimization.

### node/
Multi-chain P2P networking. Connects across Ethereum L1 + L2s using devp2p + libp2p hybrid transport. Mempool subscription and flow entropy extraction.

### arb/
Multi-hop arbitrage detection and execution. Strategies: same-chain arb, cross-chain, liquidation, backrun. Routes through DEX liquidity pools with flash loans.

### rewards/
Epoch-based reward distribution. Middle-out economics for storage contributors. Merkle proof verification and on-chain finalization.

### content/
IPFS-like content distribution over libp2p. Merkle DAG chunking, provider registry, gossip announcements.

### memory/
Engram-based associative memory. Distributed activation via pathways with decay mechanics.

### grammar/
NLP parsing and sentence analysis. Tokenization, POS tagging, intent extraction.

### attention/
Semantic attention scoring with quantum pressure modulation. Temperature-adjusted softmax over synset embeddings.

### embed/
64-dimensional synset embeddings for semantic similarity.

### contracts/
On-chain governance: ZeamRegistry for binary updates, RevenuePool for epoch rewards.

### update/
Auto-update system. Fetches manifests from ZeamRegistry, verifies signatures, applies binary updates.

### pong/
Distributed Pong game demo. Deterministic computation using quantum entropy.

### app/
Tauri v2 desktop application wrapper.

## Requirements

- Go 1.24+
- ~100MB RAM for OEWN dictionary

## Build

```bash
go build -o zeam ./cmd/
```

## Usage

```bash
./zeam [flags]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-oewn` | `oewn.json` | Path to OEWN JSON file |
| `-port` | `8080` | HTTP server port |
| `-data` | `~/.zeam` | Data directory |
| `-p2p-listen` | `:30313` | P2P listen address |
| `-p2p-peer` | | Bootstrap peer enode URL |
| `-relay` | `true` | Use libp2p relay for NAT traversal |
| `-testnet` | `false` | Use testnet chains |
| `-no-p2p` | `false` | Disable P2P networking |
| `-cli` | `false` | Run in CLI mode |
| `-passkey` | `false` | Use passkey encryption |
| `-new` | `false` | Create new identity |
| `-ipc` | | Unix socket path for IPC |

### Examples

```bash
./zeam -port 8080 -testnet

./zeam -cli -passkey -new

./zeam -p2p-peer enode://abc123...@127.0.0.1:30305
```

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `/auth` | Identity authentication |
| `/generate` | NGAC text generation |
| `/hash` | Hash computation |
| `/collapse` | Quantum collapse |
| `/pong` | Distributed Pong game |
| `/arb` | Arbitrage detection |
| `/rewards` | Epoch rewards |
| `/content` | Content addressing |

## Pressure Metrics

All cognition flows through a 4-dimensional pressure vector:

```go
type PressureMetrics struct {
    Hadamard float64
    PauliX   float64
    PauliZ   float64
    Phase    float64
}
```

Pressure accumulates from:
- Pending transaction count
- Block profitability
- Flow entropy from network
- Input text characteristics

## Stateless Identity

Identity is derived deterministically:

```
User Secret + Chain Salt → ECDSA Key → Fork ID
```

No backup files required. Same identity recoverable from secret + salt.

## Multi-Chain Support

| Chain | Transport |
|-------|-----------|
| Ethereum | devp2p |
| Optimism | libp2p + devp2p |
| Base | libp2p + devp2p |
| Arbitrum | libp2p + devp2p |
| Sepolia | devp2p |

## Arbitrage Strategies

| Strategy | Description |
|----------|-------------|
| Arb | Same-chain multi-hop through DEX pools |
| CrossChain | Bridge arbitrage across L1/L2 |
| Liquidation | Collateral liquidation opportunities |
| Backrun | Transaction backrunning |

## Reward System

Epoch-based distribution:
1. Revenue accumulates from arbitrage profits
2. Storage contributors earn based on byte-hours + challenge rate
3. Merkle tree generated at epoch end
4. On-chain finalization via RevenuePool contract

---

*ZEAM: Web4 infrastructure for decentralized cognition.*

