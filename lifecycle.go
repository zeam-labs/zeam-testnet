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

	agent := &Agent{
		ID:       id,
		Parent:   parent,
		Mode:     "cold",
		LastMint: time.Now(),
		Surplus:  1.0,
		ShardMap: LoadShardMap(Chains["civicL1"]),
		L2:       l2,
		L3:       l3,
	}
	ActiveAgents[id] = agent

	Chains["cognitionL1"].Mint(ctx, Input{
		Type:      "spawn",
		Source:    "agent",
		Content:   fmt.Sprintf("agent_spawned | ID: %s | Name: %s | Parent: %s", id, name, parent),
		Timestamp: time.Now().UTC(),
	})
	return nil
}

func AgentRuntimeController(agent *Agent) {
	if agent.Surplus <= 0 {
		agent.Mode = "cold"
		return
	}
	if time.Since(agent.LastMint) > 1*time.Minute {
		RunZARPass(agent)
		agent.LastMint = time.Now()
	}
	if agent.Mode == "hot" {
		RunAgentCortex(agent)
	}
}

func RunAgentCortex(agent *Agent) {
	ctx := assembleContext(agent.L2, agent.L3)
	result := RunLLMFromWASM("", ctx, runtime)
	if strings.TrimSpace(result) != "" {
		agent.L2.Mint(context.Background(), Input{
			Content:   result,
			Source:    agent.ID,
			Timestamp: time.Now().UTC(),
		})
	}
}

func RunZARPass(agent *Agent) {

	snapshot := []string{}
	for _, e := range agent.L2.Entries {
		snapshot = append(snapshot, e.Content)
	}
	combined := strings.Join(snapshot, "\n")

	result := RunLLMFromWASM(combined, CORE_CONTEXT, runtime)
	if strings.TrimSpace(result) != "" {
		agent.L3.Mint(context.Background(), Input{
			Content:   "trait:" + result,
			Source:    agent.ID,
			Timestamp: time.Now().UTC(),
		})
	}
}

func SpawnPresence(ctx context.Context, id, name string) error {
	l2 := NewChain("presence:L2:" + id)
	l3 := NewChain("presence:L3:" + id)

	Chains["presence:L2:"+id] = l2
	Chains["presence:L3:"+id] = l3

	sbm := uuid.New().String()
	biometric := id

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
		Content:   fmt.Sprintf("presence_spawned | ID: %s | Name: %s | SBM: %s | biometric_hash: %s", id, name, sbm, biometric),
		Timestamp: time.Now().UTC(),
	})

	return nil
}

func PresenceRuntimeController(p *Presence) {
	if p.Surplus <= 0 {
		p.Mode = "cold"
		return
	}
	if time.Since(p.LastMint) > 1*time.Minute {
		RunPresenceCortex(p)
		p.LastMint = time.Now()
	}
	if p.Mode == "hot" {
		RunPresenceCortex(p)
	}
}

func RunPresenceCortex(p *Presence) {
	ctx := assembleContext(p.L2, p.L3)

	result := RunLLMFromWASM("", ctx, runtime)
	if strings.TrimSpace(result) != "" {
		p.L2.Mint(context.Background(), Input{
			Content:   result,
			Source:    p.ID,
			Timestamp: time.Now().UTC(),
		})
	}

	snapshot := []string{}
	for _, e := range p.L2.Entries {
		snapshot = append(snapshot, e.Content)
	}
	traitInput := strings.Join(snapshot, "\n")

	traitResult := RunLLMFromWASM(traitInput, CORE_CONTEXT, runtime)
	if strings.TrimSpace(traitResult) != "" {
		p.L3.Mint(context.Background(), Input{
			Content:   "trait:" + traitResult,
			Source:    p.ID,
			Timestamp: time.Now().UTC(),
		})
	}
}

func assembleContext(l2 *Chain, l3 *Chain) string {
	var l2Excerpt string
	for i := len(l2.Entries) - 1; i >= 0 && len(l2Excerpt) < 1000; i-- {
		entry := l2.Entries[i]
		if entry.Type == "reflect" || entry.Type == "affirm" || entry.Type == "observe" {
			l2Excerpt += fmt.Sprintf("- %s\n", entry.Content)
		}
	}

	var l3Tension string
	for _, entry := range l3.Entries {
		if strings.Contains(entry.Content, "pressure") || strings.Contains(entry.Content, "drift") {
			l3Tension += fmt.Sprintf("* %s\n", entry.Content)
		}
	}

	return strings.Join([]string{
		CORE_CONTEXT,
		"\n\n--- Recent Reflections ---\n" + l2Excerpt,
		"\n\n--- Trait Pressure ---\n" + l3Tension,
	}, "\n\n")
}
