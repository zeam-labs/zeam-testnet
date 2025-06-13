package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	//"github.com/tetratelabs/wazero/api"
)

var CORE_CONTEXT string
var CORE_PRINCIPLES string
var TRAIT_MANIFEST string
var PROTOCOLS string

var ActiveAgents = map[string]*Agent{}
var ActivePresences = map[string]*Presence{}
var Chains = map[string]*Chain{}
var Vaults = map[string]float64{}

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hashString(s string) string {
	return hashBytes([]byte(s))
}

func getNowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func cleanupTemp(path string) {
	_ = os.Remove(path)
}

func pinToIPFS(data []byte) (string, error) {
	tmp, err := os.CreateTemp("", "ipfs-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		return "", err
	}
	tmp.Close()

	out, err := exec.Command("ipfs", "add", "-Q", tmp.Name()).Output()
	if err != nil {
		return "", fmt.Errorf("ipfs pin error: %v", err)
	}
	return strings.TrimSpace(string(out)), nil
}

type IPFSShardReader struct {
	ShardNames []string
	CIDMap     map[string]string
	current    int
}

func NewIPFSShardReader(shardMap map[string]string) *IPFSShardReader {
	var names []string
	for name := range shardMap {
		if strings.HasPrefix(name, "gguf-shard-") {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	return &IPFSShardReader{
		ShardNames: names,
		CIDMap:     shardMap,
		current:    0,
	}
}

func (s *IPFSShardReader) LoadNext() ([]byte, error) {
	if s.current >= len(s.ShardNames) {
		return nil, io.EOF
	}
	name := s.ShardNames[s.current]
	cid := s.CIDMap[name]

	data, err := readFromIPFS(cid)
	if err != nil {
		return nil, fmt.Errorf("failed to read shard %s (CID %s): %v", name, cid, err)
	}

	fmt.Printf("✅ Loaded shard %s → %d bytes\n", name, len(data))
	s.current++
	return data, nil
}

func (s *IPFSShardReader) Reset() {
	s.current = 0
}

func readFromIPFS(cid string) ([]byte, error) {
	out, err := exec.Command("ipfs", "cat", cid).Output()
	if err != nil {
		return nil, fmt.Errorf("ipfs read error: %v", err)
	}
	return out, nil
}

func LoadShardMap(c *Chain) map[string]string {
	shardMap := make(map[string]string)

	for _, entry := range c.Entries {
		if strings.HasPrefix(entry.Content, "shard_index:") && entry.Source == "ignite" {
			raw := strings.TrimPrefix(entry.Content, "shard_index:")
			if err := json.Unmarshal([]byte(raw), &shardMap); err != nil {
				fmt.Println("Failed to unmarshal shard_index:", err)
			}
			break
		}
	}

	return shardMap
}

func LoadWASMMap(c *Chain) map[string]string {
	wasmMap := make(map[string]string)

	for _, entry := range c.Entries {
		if strings.HasPrefix(entry.Content, "wasm_index:") && entry.Source == "ignite" {
			raw := strings.TrimPrefix(entry.Content, "wasm_index:")
			if err := json.Unmarshal([]byte(raw), &wasmMap); err != nil {
				fmt.Println("Failed to unmarshal wasm_index:", err)
			}
			break
		}
	}

	return wasmMap
}

func NewChain(name string) *Chain {
	return &Chain{Name: name, Entries: []Input{}}
}

func LoadChain(name string) *Chain {
	return &Chain{Name: name, Entries: []Input{}}
}

func GenerateClientFingerprint() string {
	now := time.Now().UnixNano()
	return fmt.Sprintf("client-%x", sha256.Sum256([]byte(fmt.Sprint(now))))
}

func StartCivicStorageLoop(shardMap map[string]string, clientID string) {
	go func() {
		for {
			assigned := AssignShardsToClient(shardMap, clientID, 3, Chains["civicL4"], Chains["cognitionL4"])
			for _, cid := range assigned {
				_ = exec.Command("ipfs", "pin", "add", cid).Run()
			}
			time.Sleep(5 * time.Minute)
		}
	}()
}

func StartCivicComputeLoop(civicL4, cognitionL4 *Chain) {
	go func() {
		for {
			tasks := FetchCivicTasks(civicL4, cognitionL4)
			for _, t := range tasks {
				switch t.Type {
				case "agent":
					if a, ok := ActiveAgents[t.ID]; ok {
						RunZARPass(a)
					}
				case "presence":
					if p, ok := ActivePresences[t.ID]; ok {
						RunPresenceCortex(p)
					}
				}
			}
			time.Sleep(30 * time.Second)
		}
	}()
}

func (c *Chain) Mint(ctx context.Context, input Input) {
	c.Entries = append(c.Entries, input)
}

func RunLLM(surface, context string) string {
	return RunLLMFromWASM(surface, context, runtime)
}

func RunLLMFromWASM(prompt, contextStr string, c *Cortex) string {
	ctx := context.Background()

	uniqueID := fmt.Sprintf("runner-%d", time.Now().UnixNano())

	module, err := c.wasmRuntime.InstantiateModule(ctx, c.wasmCompiled,
		wazero.NewModuleConfig().WithName(uniqueID),
	)

	if err != nil {
		fmt.Println("WASM instantiation failed:", err)
		return "ERROR: wasm instantiation failed"
	}

	mem := module.Memory()
	allocate := module.ExportedFunction("allocate")
	runFn := module.ExportedFunction("run_mistral")
	lenFn := module.ExportedFunction("response_len")
	if mem == nil || allocate == nil || runFn == nil || lenFn == nil {
		return "ERROR: missing wasm exports"
	}

	writeToWASM := func(data []byte) (uint32, error) {
		res, err := allocate.Call(ctx, uint64(len(data)))
		if err != nil || len(res) == 0 {
			return 0, fmt.Errorf("allocate failed")
		}
		ptr := uint32(res[0])
		if !mem.Write(ptr, data) {
			return 0, fmt.Errorf("mem write failed")
		}
		return ptr, nil
	}

	modelPtr, err := writeToWASM(c.ggufModel)
	if err != nil {
		return "ERROR: failed to write model"
	}

	fullPrompt := contextStr + "\n" + prompt
	promptPtr, err := writeToWASM([]byte(fullPrompt))
	if err != nil {
		return "ERROR: failed to write prompt"
	}

	results, err := runFn.Call(ctx,
		uint64(modelPtr), uint64(len(c.ggufModel)),
		uint64(promptPtr), uint64(len(fullPrompt)),
	)
	if err != nil || len(results) == 0 {
		return "ERROR: wasm execution failed"
	}
	resultPtr := uint32(results[0])

	lenResults, err := lenFn.Call(ctx)
	if err != nil || len(lenResults) == 0 {
		return "ERROR: response_len failed"
	}
	resultLen := uint32(lenResults[0])

	output, ok := mem.Read(resultPtr, resultLen)
	if !ok {
		return "ERROR: mem read failed"
	}

	return string(output)
}

func joinChain(chain *Chain) string {
	var lines []string
	for _, e := range chain.Entries {
		lines = append(lines, e.Content)
	}
	return strings.Join(lines, "\n")
}
