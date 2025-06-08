package main

import (
	"context"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func StartCortex(
	civicL1, cognitionL1, civicL4, cognitionL4, civicL5, cognitionL5, civicL6, cognitionL6 *Chain,
	shardMap map[string]string,
	) *Cortex {
	c := &Cortex{
		vm:           lua.NewState(),
		civicL1:      civicL1,
		cognitionL1:  cognitionL1,
		civicL4:      civicL4,
		cognitionL4:  cognitionL4,
		civicL5:      civicL5,
		cognitionL5:  cognitionL5,
		civicL6:      civicL6,
		cognitionL6:  cognitionL6,
		shardMap:     shardMap,
	}
	c.loadCoreDocuments()
	c.loadAndVerifyShards()
	c.injectContext()
	return c
}

func (c *Cortex) Interpret(input Input) {
	c.vm.SetGlobal("SURFACE", lua.LString(input.Content))
	c.vm.DoString(`response = interpret(SURFACE, CORE_CONTEXT)`)

	resp := c.vm.GetGlobal("response").String()
	parts := strings.SplitN(resp, "|", 2)
	if len(parts) != 2 {
		return 
	}

	meta := strings.TrimSpace(parts[0])
	content := strings.TrimSpace(parts[1])

	metaParts := strings.SplitN(meta, ":", 2)
	if len(metaParts) != 2 || metaParts[0] != "mint" {
		return 
	}

	layerKey := metaParts[1] 
	chain := Chains[layerKey]
	if chain == nil {
		return
	}

	chain.Mint(context.Background(), Input{
		Content:   content,
		Timestamp: time.Now().UTC(),
		Source:    "cortex",
	})

	c.Output = append(c.Output, content)
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

	c.context = core + "\n\n" + traits + "\n\n" + protos

	CORE_CONTEXT = c.context
	TRAIT_MANIFEST = traits

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
		mint(Chains["civicL6"], "Invalid core document format: " + label, "ZEAM_SYSTEM")
		return ""
	}

	hashLine := strings.TrimPrefix(lines[0], label+":")
	content := lines[1]

	if hashString(content) != strings.TrimSpace(hashLine) {
		mint(Chains["civicL6"], "Core doc failed verification: " + label, "ZEAM_SYSTEM")
		return ""
	}

	return content
}

func (c *Cortex) loadAndVerifyShards() {
	for name, cid := range c.shardMap {
		data, err := readFromIPFS(cid)
		if err != nil || hashBytes(data) != cid {
			mint(Chains["civicL6"], "SHARD HASH MISMATCH: " + name, "ZEAM_SYSTEM")
			continue
		}
		c.vm.DoString(string(data))
	}
}

func (c *Cortex) injectContext() {
	c.vm.SetGlobal("CORE_CONTEXT", lua.LString(c.context))
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
						RunPresenceTraitPass(p)
					}
				}
			}
			time.Sleep(1 * time.Minute)
		}
	}()
}
