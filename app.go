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

func mintError(entry string, civicL6, cognitionL6 *App) {
	ctx := context.Background()
	civicL6.Mint(ctx, entry)
	cognitionL6.Mint(ctx, entry)
}

type App struct {
	vm       *lua.LState
	context  string
	shardDir string
	civicL1  []string
	cogntionL1    []string
	civicL4  []string
	cognitionL4    []string
}

func NewApp(civicL1, cognitionL1, civicL4, cognitionL4 []string, shardDir string) *App {
	app := &App{
		vm:       lua.NewState(),
		shardDir: shardDir,
		civicL1:  civicL1,
		cogL1:    cognitionL1,
		civicL4:  civicL4,
		cogL4:    cognitionL4,
	}
	app.loadCoreDocuments()
	app.loadAndVerifyShards()
	app.injectContext()
	return app
}

func (a *App) loadCoreDocuments() {
	core := extractFromL1("Immutable Core", a.civicL1, a.cognitionL1)
	traits := extractFromL1("Trait Manifest", a.civicL1, a.cognitionL1)
	proto := extractFromL1("Protocols", a.civicL1, a.cognitionL1)
	a.context = core + "\n\n" + traits + "\n\n" + proto
}

func extractFromL1(label string, civicL1, cognitionL1 []string) string {
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

func extractShardHashes(civicL4, cognitionL4 []string) []string {
	seen := make(map[string]bool)
	var hashes []string
	for _, e := range civicL4 {
		if strings.HasPrefix(e, "shard_hash:") {
			h := strings.TrimPrefix(e, "shard_hash:")
			seen[h] = true
		}
	}
	for _, e := range cognitionL4 {
		if strings.HasPrefix(e, "shard_hash:") {
			h := strings.TrimPrefix(e, "shard_hash:")
			if seen[h] {
				hashes = append(hashes, h)
			}
		}
	}
	return hashes
}

func (a *App) loadAndVerifyShards() {
	shardHashes := extractShardHashes(a.civicL4, a.cognitionL4)

	for _, hash := range shardHashes {
		data := ipfs.FetchShard(hash)
		if !ipfs.VerifyShard(data, hash) {
			mintError("SHARD HASH MISMATCH: "+hash, a.civicL6, a.cognitionL6)
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

func (a *App) injectContext() {
	a.vm.SetGlobal("CORE_CONTEXT", lua.LString(a.context))
}

func (a *App) Interpret(surface string) {
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

func hash(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}
