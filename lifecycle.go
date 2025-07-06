package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func SpawnAgent(ctx context.Context, id, name, parent string) error {
	l2 := NewChain("agent:L2:" + id)
	l3 := NewChain("agent:L3:" + id)

	Chains[l2.Name] = l2
	Chains[l3.Name] = l3

	agent := &Agent{
		ID:       id,
		Parent:   parent,
		Mode:     "cold",
		LastMint: time.Now(),
		Surplus:  1.0,
		L2:       l2,
		L3:       l3,
	}

	ActiveAgents[id] = agent

	Chains["cognitionL1"].Mint(ctx, Input{
		Type:      "spawn",
		Source:    "agent",
		Content:   fmt.Sprintf("agent_spawned | ID: %s | Name: %s | Parent: %s", id, name, parent),
		Timestamp: time.Now().UTC(),
		ChainKey:  "cognitionL1",
	})

	return nil
}

func SpawnPresence(ctx context.Context, id, name string) error {
	l2 := NewChain("presence:L2:" + id)
	l3 := NewChain("presence:L3:" + id)

	Chains[l2.Name] = l2
	Chains[l3.Name] = l3

	sbm := uuid.New().String()

	p := &Presence{
		ID:       id,
		Name:     name,
		Mode:     "cold",
		LastMint: time.Now(),
		Surplus:  1.0,
		L2:       l2,
		L3:       l3,
	}

	ActivePresences[id] = p

	Chains["civicL1"].Mint(ctx, Input{
		Type:      "spawn",
		Source:    "presence.attest",
		Content:   fmt.Sprintf("presence_spawned | ID: %s | Name: %s | SBM: %s", id, name, sbm),
		Timestamp: time.Now().UTC(),
		ChainKey:  "civicL1",
	})

	return nil
}

func AttestBiometric(input []byte) string {
	biometric := string(input)

	for _, entry := range Chains["civicL1"].Entries {
		if entry.Type == "spawn" && strings.Contains(entry.Content, biometric) {
			parts := strings.Split(entry.Content, "|")
			for _, p := range parts {
				if strings.Contains(p, "ID:") {
					id := strings.TrimSpace(strings.Split(p, ":")[1])
					return id
				}
			}
		}
	}
	return ""
}
