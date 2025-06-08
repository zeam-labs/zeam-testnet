package main

import (
	"time"

	lua "github.com/yuin/gopher-lua"
)

type Input struct {
	Source    string    `json:"source"`
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
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
	ShardMap map[string]string
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
	vm          *lua.LState
	context     string
	shardMap    map[string]string

	civicL1     *Chain
	cognitionL1 *Chain
	civicL4     *Chain
	cognitionL4 *Chain
	civicL5     *Chain
	cognitionL5 *Chain
	civicL6     *Chain
	cognitionL6 *Chain

	Output      []string
}

type CivicTask struct {
	ID   string
	Type string
}

type Chain struct {
	Name    string
	Entries []Input
}
