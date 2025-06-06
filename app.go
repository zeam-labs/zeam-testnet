package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/ioutil"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"zeam/ipfs"
	"zeam/x/reflect"
	"zeam/x/affirm"
	"zeam/x/anchor"
	"zeam/x/observe"
	"zeam/x/offer"
	"zeam/x/store"
)

var Chains = map[string]*Chain{}
var Output = make(chan string, 100)

type Chain struct {
	ID string
	Log []Input
}

type Cortex struct {
	vm           *lua.LState
	context      string
	civicL1      *Chain
	cognitionL1  *Chain
	civicL4      *Chain
	cognitionL4  *Chain
	civicL5      *Chain
	cognitionL5  *Chain
	civicL6      *Chain
	cognitionL6  *Chain
}

type PresenceContext struct {
	Presence *Chain
	Traits   map[string]*Chain
}

func StartCortex(civicL1, cognitionL1, civicL4, cognitionL4 []string, shardDir string) *App {
	app := &Cortex{
		vm:       lua.NewState(),
		shardDir: shardDir,
		civicL1:  civicL1,
		cognitionL1:    cognitionL1,
		civicL4:  civicL4,
		cognitionL4:    cognitionL4,
	}
	app.loadCoreDocuments()
	app.loadAndVerifyShards()
	app.injectContext()
	return Cortex
}

func (c *Cortex) ActivatePresence(presenceID string) *PresenceContext {
	p := LoadPresence(presenceID)
	traits := LoadTraitSet(presenceID)
	return &PresenceContext{
		Presence: p,
		Traits: traits,
	}
}

func handleAgentSpawn(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()
	var req spawnRequest
	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	switch req.Type {
	case "presence":
		presence_spawner.SpawnPresence(r.Context(), presence_spawner.PresenceParams{
			PresenceID:   req.ID,
			AssignedName: req.Name,
		})
	case "agent":
		agent_spawner.SpawnAgent(r.Context(), agent_spawner.AgentParams{
			AgentID:      req.ID,
			AssignedName: req.Name,
			AssignedTo:   req.PresenceID,
		})
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func mintError(entry string, civicL6, cognitionL6 *App) {
	ctx := context.Background()
	civicL6.Mint(ctx, entry)
	cognitionL6.Mint(ctx, entry)
}

func (a *App) loadCoreDocuments() {
	core := extractFromL1("Immutable Core", a.civicL1, a.cognitionL1)
	traits := extractFromL1("Trait Manifest", a.civicL1, a.cognitionL1)
	proto := extractFromL1("Protocols", a.civicL1, a.cognitionL1)
	a.context = core + "\n\n" + traits + "\n\n" + proto
}

func extractCoreDocs(label string, civicL1, cognitionL1 []string) string {
	var civicDoc, cogDoc string
	for _, e := range civicL1 {
		if strings.HasPrefix(e, label+":") {
			civicDoc = e
			break
		}
	}
	for _, e := range cogL1 {
		if strings.HasPrefix(e, label+":") {
			cogDoc = e
			break
		}
	}
	if civicDoc == "" || cogDoc == "" || civicDoc != cogDoc {
		mintError("Core documents are mismatched or missing", app.civicL6, app.cognitionL6)
	}
	lines := strings.SplitN(civicDoc, "\n", 2)
	if len(lines) < 2 {
		mintError("Invalid core document format", app.civicL6, app.cognitionL6)
	}
	hashLine := strings.TrimPrefix(lines[0], label+":")
	content := lines[1]
	if hash(content) != strings.TrimSpace(hashLine) {
		mintError("Core doc failed verification: " + label, app.civicL6, app.cognitionL6)
	}
	return content
}

func (a *Cortex) loadAndVerifyShards() {
	for name, cid := range a.shardMap {
		data := ipfs.FetchShard(cid)

		if !ipfs.VerifyShard(data, cid) {
			mintError("SHARD HASH MISMATCH: "+cid, a.civicL6, a.cognitionL6)
			continue
		}
		a.vm.DoString(string(data)) 
	}

}

func containsHash(h string, chain []string) bool {
	for _, e := range chain {
		if strings.Contains(e, h) {
			return true
		}
	}
	return false
}

func (a *Cortex) injectContext() {
	a.vm.SetGlobal("CORE_CONTEXT", lua.LString(a.context))
}

func (a *Cortex) Interpret(surface string) {
	a.vm.SetGlobal("SURFACE", lua.LString(surface))
	a.vm.DoString(`response = interpret(SURFACE, CORE_CONTEXT)`)

	resp := a.vm.GetGlobal("response").String()

	switch {
	case strings.HasPrefix(resp, "reflect:"):
		reflect.Run(resp)
	case strings.HasPrefix(resp, "affirm:"):
		affirm.Run(resp)
	case strings.HasPrefix(resp, "anchor:"):
		anchor.Run(resp)
	case strings.HasPrefix(resp, "observe:"):
		observe.Run(resp)
	case strings.HasPrefix(resp, "offer:"):
		offer.Run(resp)
	default:
		store.Run(resp)
	}
}

func (a *Cortex) Interpret(input Input) {
    reflexType, targetChain := a.RunLLM(input)

    switch reflexType {
    case "observe":
        observe.Execute(context.Background(), input.Content, targetChain)
    case "reflect":
        reflect.Execute(context.Background(), input.Content, targetChain)
    case "offer":
        offer.Execute(context.Background(), input.Content, targetChain)
    case "anchor":
        anchor.Execute(context.Background(), input.Content, targetChain)
    case "affirm":
        affirm.Execute(context.Background(), input.Content, targetChain)
    case "store":
        store.Execute(context.Background(), input.Content, targetChain)
    default:
    }
}

func hash(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}
