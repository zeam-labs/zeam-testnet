package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
)

func StartCortex(
	civicL1, cognitionL1, civicL4, cognitionL4, civicL5, cognitionL5, civicL6, cognitionL6 *Chain,
	shardMap map[string]string,
	wasmMap map[string]string,
) *Cortex {
	c := &Cortex{
		civicL1:     civicL1,
		cognitionL1: cognitionL1,
		civicL4:     civicL4,
		cognitionL4: cognitionL4,
		civicL5:     civicL5,
		cognitionL5: cognitionL5,
		civicL6:     civicL6,
		cognitionL6: cognitionL6,
		shardMap:    shardMap,
		wasmMap:     wasmMap,
	}

	reader := NewIPFSShardReader(shardMap)

	cid, ok := wasmMap["model_runner.wasm"]
	if !ok {
		panic("model_runner.wasm not found in wasmMap")
	}
	wasmBytes, err := readFromIPFS(cid)
	if err != nil {
		panic(fmt.Sprintf("Failed to read WASM: %v", err))
	}

	runtime := wazero.NewRuntime(context.Background())
	compiled, err := runtime.CompileModule(context.Background(), wasmBytes)
	if err != nil {
		panic(fmt.Sprintf("Failed to compile WASM: %v", err))
	}

	c.ShardReader = reader
	c.wasmRuntime = runtime
	c.wasmCompiled = compiled

	c.loadCoreDocuments()

	return c
}

func (c *Cortex) Interpret(input Input) {
	surface := strings.TrimSpace(input.Content)
	if surface == "" {
		return
	}

	result := RunLLMFromWASM(surface, c.context, runtime)
	if strings.TrimSpace(result) == "" {
		result = "Acknowledged"
	}

	origin := input.ChainKey
	chain := Chains[origin]
	if chain == nil {
		return
	}

	chain.Mint(context.Background(), Input{
		Content:   result,
		Timestamp: time.Now().UTC(),
		Source:    "cortex",
		Type:      "reflect",
		ChainKey:  origin,
	})

	c.Output = append(c.Output, result)
}

func mint(chain *Chain, content string, source string) {
	if chain == nil {
		return
	}
	chain.Mint(context.Background(), Input{
		Content:   content,
		Source:    source,
		Timestamp: time.Now().UTC(),
	})
}

func (c *Cortex) loadCoreDocuments() {
	core := extractCoreDocs("core_hash", c.civicL1, c.cognitionL1)
	traits := extractCoreDocs("trait_hash", c.civicL1, c.cognitionL1)
	protos := extractCoreDocs("protocol_hash", c.civicL1, c.cognitionL1)

	CORE_PRINCIPLES = core
	TRAIT_MANIFEST = traits
	PROTOCOLS = protos

	CORE_CONTEXT = core + "\n\n" + traits + "\n\n" + protos

	c.context = CORE_CONTEXT
}

func extractCoreDocs(label string, civicL1, cognitionL1 *Chain) string {
	var docCivic, docCog string
	for _, e := range civicL1.Entries {
		if strings.HasPrefix(e.Content, label+":") {
			docCivic = e.Content
			break
		}
	}
	for _, e := range cognitionL1.Entries {
		if strings.HasPrefix(e.Content, label+":") {
			docCog = e.Content
			break
		}
	}

	if docCivic == "" || docCog == "" || docCivic != docCog {
		mint(Chains["civicL6"], "Core documents are mismatched or missing", "ZEAM_SYSTEM")
		return ""
	}

	lines := strings.SplitN(docCivic, "\n", 2)
	if len(lines) < 2 {
		mint(Chains["civicL6"], "Invalid core document format: "+label, "ZEAM_SYSTEM")
		return ""
	}

	hashLine := strings.TrimPrefix(lines[0], label+":")
	content := lines[1]

	if hashString(content) != strings.TrimSpace(hashLine) {
		mint(Chains["civicL6"], "Core doc failed verification: "+label, "ZEAM_SYSTEM")
		return ""
	}
	fmt.Printf("extractCoreDocs → Label: %s\n", label)
	fmt.Printf("Civic Entry:\n%s\n", docCivic)
	fmt.Printf("Cognition Entry:\n%s\n", docCog)

	return content
}

func FetchCivicTasks(l4s ...*Chain) []CivicTask {
	var tasks []CivicTask

	for _, l4 := range l4s {
		for _, entry := range l4.Entries {
			if entry.Type == "civic.task" && strings.HasPrefix(entry.Content, "run:") {
				parts := strings.Split(entry.Content, ":")
				if len(parts) == 3 {
					tasks = append(tasks, CivicTask{
						Type: parts[1],
						ID:   parts[2],
					})
				}
			}
		}
	}

	return tasks
}

func StartCivicComputeWorker(civicL4, cognitionL4 *Chain) {
	go func() {
		for {
			tasks := FetchCivicTasks(civicL4, cognitionL4)
			for _, t := range tasks {
				switch t.Type {
				case "agent":
					if agent, ok := ActiveAgents[t.ID]; ok {
						RunZARPass(agent)
					}
				case "presence":
					if p, ok := ActivePresences[t.ID]; ok {
						RunPresenceCortex(p)
					}
				}
			}
			time.Sleep(1 * time.Minute)
		}
	}()
}
