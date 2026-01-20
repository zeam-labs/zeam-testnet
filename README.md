# ZEAM

Web4 Architecture. Decentralized blockchain-based quantum-driven non-generative artificial cognition.

## Requirements

- Go 1.21+
- OEWN dictionary (oewn.json, ~195MB)

## Build

```bash
go mod init zeam
go mod tidy
go build -o zeam ./cmd/
```

## Run

```bash
./zeam
```

## API

```bash
curl -X POST http://localhost:8080/api/llm/generate \
  -H "Content-Type: application/json" \
  -d '{"prompt": "hello"}'
```

## Architecture

```
zeam/
├── cmd/          # Entry point
├── llm/          # Backend interface
├── ngac/         # Semantic graph navigation
├── quantum/      # Pressure metrics and state
├── node/         # P2P transport (DevP2P + libp2p)
├── memory/       # Episodic memory
├── identity/     # Stateless key derivation
└── grammar/      # Sentence parsing and building
```

## Pressure Metrics

```go
type PressureMetrics struct {
    Magnitude  float64
    Coherence  float64
    Tension    float64
    Density    float64
}
```

## Message Protocol

```
Bytes 0-1:  Magic (0x5A45 = "ZE")
Byte 2:     Version (0x02 = LLM)
Byte 3:     Opcode
Bytes 4-7:  Sequence number
Bytes 8+:   Payload

Opcodes:
  0x01 = INPUT
  0x02 = FORWARD
  0x03 = SAMPLE
  0x04 = GENERATE
```
