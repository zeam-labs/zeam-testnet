package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
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
	vm := lua.NewState()
	ctx := assembleContext(agent.L2, agent.L3)
	vm.SetGlobal("CORE_CONTEXT", lua.LString(ctx))
	vm.DoString(`response = interpret(SURFACE, CORE_CONTEXT)`)
	resp := vm.GetGlobal("response").String()
	if resp != "" {
		agent.L2.Mint(context.Background(), Input{
			Content:   resp,
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
	vm := lua.NewState()
	vm.SetGlobal("L2", lua.LString(strings.Join(snapshot, "\n")))
	vm.SetGlobal("TRAITS", lua.LString(TRAIT_MANIFEST))
	vm.SetGlobal("CORE_CONTEXT", lua.LString(CORE_CONTEXT))
	vm.DoString(`result = interpret_traits(L2, TRAITS, CORE_CONTEXT)`)
	output := vm.GetGlobal("result").String()
	if output != "" {
		agent.L3.Mint(context.Background(), Input{
			Content:   "trait:" + output,
			Source:    agent.ID,
			Timestamp: time.Now().UTC(),
		})
	}
}

func SpawnPresence(ctx context.Context, id, name string) error {
	l2 := NewChain("presence:L2:" + id)
	l3 := NewChain("presence:L3:" + id)

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
		Source:    "presence",
		Content:   fmt.Sprintf("presence_spawned | ID: %s | Name: %s", id, name),
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
		RunPresenceTraitPass(p)
		p.LastMint = time.Now()
	}
	if p.Mode == "hot" {
		RunPresenceCortex(p)
	}
}

func RunPresenceCortex(p *Presence) {
	vm := lua.NewState()
	ctx := assembleContext(p.L2, p.L3)
	vm.SetGlobal("CORE_CONTEXT", lua.LString(ctx))
	vm.DoString(`response = interpret(SURFACE, CORE_CONTEXT)`)
	resp := vm.GetGlobal("response").String()
	if resp != "" {
		p.L2.Mint(context.Background(), Input{
			Content:   resp,
			Source:    p.ID,
			Timestamp: time.Now().UTC(),
		})
	}
}

func RunPresenceTraitPass(p *Presence) {
	snapshot := []string{}
	for _, e := range p.L2.Entries {
		snapshot = append(snapshot, e.Content)
	}
	vm := lua.NewState()
	vm.SetGlobal("L2", lua.LString(strings.Join(snapshot, "\n")))
	vm.SetGlobal("TRAITS", lua.LString(TRAIT_MANIFEST))
	vm.SetGlobal("CORE_CONTEXT", lua.LString(CORE_CONTEXT))
	vm.DoString(`result = interpret_traits(L2, TRAITS, CORE_CONTEXT)`)
	output := vm.GetGlobal("result").String()
	if output != "" {
		p.L3.Mint(context.Background(), Input{
			Content:   "trait:" + output,
			Source:    p.ID,
			Timestamp: time.Now().UTC(),
		})
	}
}

func assembleContext(l2 *Chain, l3 *Chain) string {
	return CORE_CONTEXT + "\n\ntraits:\n" + TRAIT_MANIFEST
}
