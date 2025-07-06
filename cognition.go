package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
)

type Cognition struct {
	CoreContext string
	Civic       ChainSet
	Cog         ChainSet
	WasmIndex   map[string]WasmIndex
	ShardIndex  map[string]ShardIndex
	ShardReader ShardLoader
	Runtime     wazero.Runtime
	Compiled    wazero.CompiledModule
	ClientID    []string
	StartedAt   time.Time
}

func InitCognition(
	civic ChainSet,
	cog ChainSet,
	wasmIndex map[string]WasmIndex,
	shardIndex map[string]ShardIndex,
) (*Cognition, error) {

	modelEntry, ok := wasmIndex["model_runner.wasm"]
	if !ok {
		return nil, fmt.Errorf("model_runner.wasm not found in wasmIndex")
	}

	wasmBytes, err := ZFSRead(modelEntry.CID, civic.L4, cog.L4)
	if err != nil {
		return nil, fmt.Errorf("failed to stream wasm: %v", err)
	}

	rt := wazero.NewRuntime(context.Background())
	compiled, err := rt.CompileModule(context.Background(), wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to compile wasm: %v", err)
	}

	ctx, err := ResolveCoreText(civic.L1, cog.L1)
	if err != nil {
		return nil, err
	}

	loader := NewCivicShardLoader(shardIndex)

	return &Cognition{
		CoreContext: ctx,
		Civic:       civic,
		Cog:         cog,
		WasmIndex:   wasmIndex,
		ShardIndex:  shardIndex,
		ShardReader: loader,
		Runtime:     rt,
		Compiled:    compiled,
		ClientID:    modelEntry.ClientIDs,

		StartedAt: time.Now(),
	}, nil

}

func RunCortexPass(c *Cognition, id string, l2, l3 *Chain) {
	ctx := assembleContext(c.CoreContext, l2, l3)
	resp := runLLM(c, "", ctx)

	Log("🧠 RunCortexPass: response = %q", resp)

	if strings.TrimSpace(resp) != "" {
		l2.Mint(context.Background(), Input{
			Content:   resp,
			Source:    id,
			Timestamp: time.Now().UTC(),
			ChainKey:  l2.Name,
			Type:      "",
		})
		Log("✅ Minted response to L2 for %s", id)
	} else {
		Log("⚠️ Empty response from LLM")
	}
}

func RunAnalysisPass(c *Cognition, id string, l2, l3 *Chain) {
	var snapshot []string
	for _, e := range l2.Entries {
		snapshot = append(snapshot, e.Content)
	}
	combined := strings.Join(snapshot, "\n")

	resp := runLLM(c, combined, c.CoreContext)
	if strings.TrimSpace(resp) != "" {
		l3.Mint(context.Background(), Input{
			Content:   resp,
			Source:    id,
			Timestamp: time.Now().UTC(),
		})
	}
}

func assembleContext(core string, l2, l3 *Chain) string {
	var l2Excerpt, l3Tension strings.Builder

	// Pipe through all L2 memory (no type filtering)
	for _, e := range l2.Entries {
		l2Excerpt.WriteString("- ")
		l2Excerpt.WriteString(e.Content)
		l2Excerpt.WriteString("\n")
	}

	// Still surface L3 trait pressure
	for _, e := range l3.Entries {
		if strings.Contains(e.Content, "pressure") || strings.Contains(e.Content, "drift") {
			l3Tension.WriteString("* ")
			l3Tension.WriteString(e.Content)
			l3Tension.WriteString("\n")
		}
	}

	return strings.Join([]string{
		"--- CORE ---\n" + core,
		"\n\n--- INPUT ---\n" + l2Excerpt.String(),
		"\n\n--- PRESSURE ---\n" + l3Tension.String(),
	}, "\n\n")
}

func ResolveCoreText(chains ...*Chain) (string, error) {
	labels := []string{"core_hash", "trait_hash", "protocol_hash"}
	sections := []string{}

	for _, label := range labels {
		content, err := resolveHashed(label, chains...)
		if err != nil {
			return "", fmt.Errorf("failed to resolve %s: %v", label, err)
		}
		sections = append(sections, content)
	}

	return strings.Join(sections, "\n\n"), nil
}

func resolveHashed(label string, chains ...*Chain) (string, error) {
	for _, chain := range chains {
		for _, entry := range chain.Entries {
			if strings.HasPrefix(entry.Content, label+":") {
				lines := strings.SplitN(entry.Content, "\n", 2)
				if len(lines) != 2 {
					return "", fmt.Errorf("invalid format for %s entry", label)
				}

				expectedHash := strings.TrimPrefix(lines[0], label+":")
				content := lines[1]
				actualHash := hashString(content)

				if expectedHash != actualHash {
					return "", fmt.Errorf("hash mismatch for %s", label)
				}

				return content, nil
			}
		}
	}

	return "", fmt.Errorf("label %s not found", label)
}

func runLLM(c *Cognition, prompt, contextStr string) string {
	fullPrompt := contextStr + "\n" + prompt
	ctx := context.Background()

	shardCount := 0
	success := false

	// Only reset ONCE per cognition pass
	c.ShardReader.Reset()

	for {
		shard, err := c.ShardReader.LoadNext()
		if err == io.EOF {
			break
		}
		if err != nil {
			Log("⚠️ Failed to load shard: %v", err)
			continue
		}

		shardCount++

		shardBytes, err := io.ReadAll(shard)
		if err != nil {
			Log("⚠️ Failed to read shard data: %v", err)
			continue
		}

		mod, err := c.Runtime.InstantiateModule(ctx, c.Compiled, wazero.NewModuleConfig().WithName(""))
		if err != nil {
			Log("❌ WASM instantiation failed: %v", err)
			continue
		}
		defer mod.Close(ctx)

		mem := mod.Memory()
		alloc := mod.ExportedFunction("allocate")
		run := mod.ExportedFunction("run_mistral")
		flen := mod.ExportedFunction("response_len")

		write := func(data []byte) (uint32, error) {
			res, err := alloc.Call(ctx, uint64(len(data)))
			if err != nil || len(res) == 0 {
				return 0, err
			}
			ptr := uint32(res[0])
			if !mem.Write(ptr, data) {
				return 0, fmt.Errorf("mem write failed")
			}
			return ptr, nil
		}

		modelPtr, err := write(shardBytes)
		if err != nil {
			Log("⚠️ Failed to write model shard: %v", err)
			continue
		}

		promptPtr, err := write([]byte(fullPrompt))
		if err != nil {
			Log("⚠️ Failed to write prompt: %v", err)
			continue
		}

		res, err := run.Call(ctx,
			uint64(modelPtr), uint64(len(shardBytes)),
			uint64(promptPtr), uint64(len(fullPrompt)),
		)
		if err != nil || len(res) == 0 {
			Log("⚠️ WASM run failed: %v", err)
			continue
		}
		outPtr := uint32(res[0])

		olen, err := flen.Call(ctx)
		if err != nil || len(olen) == 0 {
			Log("⚠️ response_len call failed: %v", err)
			continue
		}
		outLen := uint32(olen[0])

		out, ok := mem.Read(outPtr, outLen)
		if !ok {
			Log("⚠️ Failed to read output")
			continue
		}

		text := strings.TrimSpace(string(out))
		if text != "" {
			Log("✅ Cognition success from shard %d: %s", shardCount, text)
			success = true
			return text
		}

		Log("💤 Shard %d returned no output", shardCount)
	}

	if !success {
		Log("🧘 No shard produced output after %d attempts", shardCount)
	}
	return ""
}

func ResolveShard(name string, chains ...*Chain) (*ShardIndex, error) {
	prefix := "shard_index:" + name + ":"

	for _, c := range chains {
		for _, entry := range c.Entries {
			if strings.HasPrefix(entry.Content, prefix) {
				raw := strings.TrimPrefix(entry.Content, prefix)
				var idx ShardIndex
				if err := json.Unmarshal([]byte(raw), &idx); err != nil {
					return nil, fmt.Errorf("failed to parse ShardIndex for %s: %v", name, err)
				}
				return &idx, nil
			}
		}
	}

	return nil, fmt.Errorf("shard %s not found", name)
}

func ResolveWasm(name string, chains ...*Chain) (*WasmIndex, error) {
	prefix := "wasm_index:" + name + ":"

	for _, c := range chains {
		for _, entry := range c.Entries {
			if strings.HasPrefix(entry.Content, prefix) {
				raw := strings.TrimPrefix(entry.Content, prefix)
				var idx WasmIndex
				if err := json.Unmarshal([]byte(raw), &idx); err != nil {
					return nil, fmt.Errorf("failed to parse WasmIndex for %s: %v", name, err)
				}
				return &idx, nil
			}
		}
	}

	return nil, fmt.Errorf("wasm %s not found", name)
}

func NewCivicShardLoader(ShardIndex map[string]ShardIndex) *CivicShardLoader {
	var names []string
	for name := range ShardIndex {
		if strings.HasPrefix(name, "gguf-shard-") {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	return &CivicShardLoader{
		ShardNames: names,
		CIDMap:     ShardIndex,
		current:    0,
	}
}

func (s *CivicShardLoader) LoadNext() (io.Reader, error) {
	if s.current >= len(s.ShardNames) {
		return nil, io.EOF
	}

	name := s.ShardNames[s.current]
	entry := s.CIDMap[name]
	s.current++

	data, err := fetchMeshChunk(entry.CID)
	if err != nil {
		return nil, fmt.Errorf("❌ failed to fetch shard %s (CID %s): %v", name, entry.CID, err)
	}

	return bytes.NewReader(data), nil
}

func (s *CivicShardLoader) Reset() {
	s.current = 0
}

func NewCivicWASMLoader(wasmIndex map[string]WasmIndex) *CivicWASMLoader {
	var names []string
	for name := range wasmIndex {
		if strings.HasSuffix(name, ".wasm") {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	return &CivicWASMLoader{
		ModuleNames: names,
		CIDMap:      wasmIndex,
		current:     0,
	}
}

func (w *CivicWASMLoader) LoadNext() ([]byte, string, error) {
	if w.current >= len(w.ModuleNames) {
		return nil, "", io.EOF
	}

	name := w.ModuleNames[w.current]
	entry := w.CIDMap[name]
	w.current++

	data, err := fetchMeshChunk(entry.CID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch wasm %s (CID %s): %v", name, entry.CID, err)
	}

	return data, name, nil
}

func (w *CivicWASMLoader) Reset() {
	w.current = 0
}

func ResolveCoreBundleCID(chains ...*Chain) (string, error) {
	for _, chain := range chains {
		for _, entry := range chain.Entries {
			if strings.HasPrefix(entry.Content, "core_bundle:") {
				cid := strings.TrimPrefix(entry.Content, "core_bundle:")
				if cid != "" {
					return cid, nil
				}
			}
		}
	}
	return "", fmt.Errorf("core_bundle CID not found in any chain")
}
