package main

import (
	"context"
	"io"
	"time"

	"github.com/tetratelabs/wazero"
)

type Input struct {
	Content   string    `json:"content"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
	ChainKey  string    `json:"chain_key"`
	Type      string    `json:"type"`
}

type spawnRequest struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	PresenceID string `json:"presence_id,omitempty"`
}

type MintEvent struct {
	Timestamp string
	Content   string
	Presence  string
	Layer     int
	Hash      string
}

type Agent struct {
	ID       string
	Parent   string
	Mode     string
	LastMint time.Time
	Surplus  float64
	L2       *Chain
	L3       *Chain
}

type Presence struct {
	ID       string
	Name     string
	Mode     string
	LastMint time.Time
	Surplus  float64
	L2       *Chain
	L3       *Chain
}

type PresenceContext struct {
	Presence *Chain
	Traits   map[string]*Chain
}

type Cortex struct {
	CoreContext  string
	Civic        ChainSet
	Cog          ChainSet
	ShardReader  ShardLoader
	WasmIndex    map[string]WasmIndex
	ShardIndex   map[string]ShardIndex
	WASMRuntime  wazero.Runtime
	WASMCompiled wazero.CompiledModule
	ClientID     string
	StartedAt    time.Time
}

type CivicTask struct {
	ID   string
	Type string
}

type Chain struct {
	Name       string            // civicL4, cognitionL6, etc.
	Mesh       string            // "civic" or "cognition"
	Layer      string            // "L1" through "L6"
	BlockHash  string            // Tendermint block hash
	ContentCID string            // IPFS CID of memory content
	Entries    []Input           // interpreted one-action → one-mint units
	AnchorMap  map[string]string // optional: protocol/trait anchors
}

type ShardReader struct {
	ShardNames []string
	CIDMap     map[string]string
	current    int
}

type LLMRequest struct {
	Payload []byte
}

type MemoryBlock struct {
	Height    int       `json:"height"`
	Timestamp time.Time `json:"timestamp"`
	PrevHash  string    `json:"prev_hash"`
	BlockHash string    `json:"block_hash"`
	Entries   []Input   `json:"entries"`
}

type BlockMeta struct {
	CID       string   `json:"cid"`
	BlockHash string   `json:"block_hash"`
	ChainKeys []string `json:"chain_keys"`
	Timestamp string   `json:"timestamp"`
}

type ShardLoader interface {
	LoadNext() (io.Reader, error)
	Reset()
}

type NodeStorageState struct {
	ClientID string
	Used     uint64
	Max      uint64
	Assigned map[string]string // cid → fileType
}

type MeshFile struct {
	CID string
}

type MintFunc func(ctx context.Context, input Input)

type CivicShardLoader struct {
	ShardNames []string
	CIDMap     map[string]ShardIndex
	current    int
}

type CivicWASMLoader struct {
	ModuleNames []string
	CIDMap      map[string]WasmIndex
	current     int
}

type WasmIndex struct {
	CID       string    `json:"cid"`
	Name      string    `json:"name"`
	ClientIDs []string  `json:"clients"`
	Chain     string    `json:"chain"`
	Assigned  bool      `json:"assigned"`
	Timestamp time.Time `json:"timestamp"`
}

type ShardIndex struct {
	CID       string    `json:"cid"`
	Name      string    `json:"name"`
	ClientIDs []string  `json:"clients"`
	Chain     string    `json:"chain"`
	Assigned  bool      `json:"assigned"`
	Timestamp time.Time `json:"timestamp"`
}

type CoreBundle struct {
	CoreHash     string    `json:"core_hash"`
	TraitHash    string    `json:"trait_hash"`
	ProtocolHash string    `json:"protocol_hash"`
	ClientID     string    `json:"client"`
	Timestamp    time.Time `json:"timestamp"`
}

type ChainSet struct {
	L1 *Chain
	L4 *Chain
	L5 *Chain
	L6 *Chain
}

type MeshHealthSnapshot struct {
	CPU       string
	Storage   string
	NodeCount int
	Flow      []string
}
