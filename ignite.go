package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func MintGenesis(clientID string) (map[string]WasmIndex, map[string]ShardIndex) {
	ctx := context.Background()

	chains := map[string]*Chain{
		"civicL1":     NewChain("civicL1"),
		"civicL4":     NewChain("civicL4"),
		"civicL5":     NewChain("civicL5"),
		"civicL6":     NewChain("civicL6"),
		"cognitionL1": NewChain("cognitionL1"),
		"cognitionL4": NewChain("cognitionL4"),
		"cognitionL5": NewChain("cognitionL5"),
		"cognitionL6": NewChain("cognitionL6"),
	}

	core := fetch("https://raw.githubusercontent.com/zeam-foundation/Core-Bundle/main/Immutable_Core.md")
	traits := fetch("https://raw.githubusercontent.com/zeam-foundation/Core-Bundle/main/Trait_Manifest.md")
	protocols := fetch("https://raw.githubusercontent.com/zeam-foundation/Core-Bundle/main/protocols.md")

	for _, chain := range chains {
		mintHashed := func(label, doc string) {
			chain.Mint(ctx, Input{
				Content:   fmt.Sprintf("%s:%s\n%s", label, hashString(doc), doc),
				Source:    clientID,
				Timestamp: time.Now().UTC(),
			})
		}
		mintHashed("core_hash", core)
		mintHashed("trait_hash", traits)
		mintHashed("protocol_hash", protocols)
	}

	for _, source := range chains {
		for _, target := range chains {
			if source != target {
				source.Mint(ctx, Input{
					Content:   fmt.Sprintf("anchor:%s->root", target.Name),
					Source:    clientID,
					Timestamp: time.Now().UTC(),
				})
			}
		}
	}

	coreBundle := CoreBundle{
		CoreHash:     hashString(core),
		TraitHash:    hashString(traits),
		ProtocolHash: hashString(protocols),
		ClientID:     clientID,
		Timestamp:    time.Now().UTC(),
	}

	coreJSON, _ := json.Marshal(coreBundle)
	coreCID, err := ZFSPin("core_bundle.json", coreJSON)
	if err != nil {
		panic(Log("❌ Failed to pin core bundle: %v", err))
	}

	for _, chain := range chains {
		chain.Mint(ctx, Input{
			Content:   "core_bundle:" + coreCID,
			Source:    clientID,
			Timestamp: coreBundle.Timestamp,
		})
	}

	Log("📦 Pinned core bundle → CID: %s\n", coreCID)

	shardIndex := map[string]ShardIndex{}
	if files, err := os.ReadDir("./llm"); err == nil {
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			name := f.Name()
			data, err := os.ReadFile("./llm/" + name)
			if err != nil {
				panic(Log("Failed to read shard %s: %v", name, err))
			}

			hash := hashBytes(data)
			cid, err := ZFSPin(name, data)
			if err != nil {
				panic(Log("Failed to store shard %s: %v", name, err))
			}

			index := ShardIndex{
				CID:       cid,
				Chain:     "civicL4",
				ClientIDs: []string{clientID},
				Assigned:  true,
			}

			shardIndex[name] = index

			payload, _ := json.Marshal(index)
			hashEntry := fmt.Sprintf("shard_hash:%s", hash)
			indexEntry := fmt.Sprintf("shard_index:%s:%s", name, string(payload))

			chains["civicL4"].Mint(ctx, Input{Content: hashEntry, Source: clientID, Timestamp: time.Now().UTC()})
			chains["cognitionL4"].Mint(ctx, Input{Content: hashEntry, Source: clientID, Timestamp: time.Now().UTC()})
			chains["civicL1"].Mint(ctx, Input{Content: indexEntry, Source: clientID, Timestamp: time.Now().UTC()})
			chains["cognitionL1"].Mint(ctx, Input{Content: indexEntry, Source: clientID, Timestamp: time.Now().UTC()})
			chains["civicL4"].Mint(ctx, Input{Content: indexEntry, Source: clientID, Timestamp: time.Now().UTC()})
			chains["cognitionL4"].Mint(ctx, Input{Content: indexEntry, Source: clientID, Timestamp: time.Now().UTC()})

			Log("📦 Stored shard %s → CID: %s | Hash: %s\n", name, cid, hash)
		}
	}

	for name, info := range shardIndex {
		if ZFSExists(info.CID) {
			Log("📤 Finalizing shard %s → %s\n", name, info.CID)

			info.Assigned = true
			info.ClientIDs = []string{clientID}
			info.Chain = "civicL4"

			payload, _ := json.Marshal(info)
			indexEntry := fmt.Sprintf("shard_index:%s:%s", name, string(payload))

			for _, chain := range chains {
				chain.Mint(ctx, Input{
					Type:      "shard_index",
					Source:    clientID,
					Content:   indexEntry,
					Timestamp: time.Now().UTC(),
				})
			}
		}
	}

	wasmIndex := map[string]WasmIndex{}
	if files, err := os.ReadDir("./wasm"); err == nil {
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			name := f.Name()
			data, err := os.ReadFile("./wasm/" + name)
			if err != nil {
				panic(Log("Failed to read WASM %s: %v", name, err))
			}

			hash := hashBytes(data)
			cid, err := ZFSPin(name, data)
			if err != nil {
				panic(Log("Failed to store WASM %s: %v", name, err))
			}

			index := WasmIndex{
				CID:       cid,
				Chain:     "civicL4",
				ClientIDs: []string{clientID},
				Assigned:  true,
			}
			wasmIndex[name] = index

			payload, _ := json.Marshal(index)
			hashEntry := fmt.Sprintf("wasm_hash:%s", hash)
			indexEntry := fmt.Sprintf("wasm_index:%s:%s", name, string(payload))

			chains["civicL4"].Mint(ctx, Input{Content: hashEntry, Source: clientID, Timestamp: time.Now().UTC()})
			chains["cognitionL4"].Mint(ctx, Input{Content: hashEntry, Source: clientID, Timestamp: time.Now().UTC()})
			chains["civicL1"].Mint(ctx, Input{Content: indexEntry, Source: clientID, Timestamp: time.Now().UTC()})
			chains["cognitionL1"].Mint(ctx, Input{Content: indexEntry, Source: clientID, Timestamp: time.Now().UTC()})
			chains["civicL4"].Mint(ctx, Input{Content: indexEntry, Source: clientID, Timestamp: time.Now().UTC()})
			chains["cognitionL4"].Mint(ctx, Input{Content: indexEntry, Source: clientID, Timestamp: time.Now().UTC()})

			Log("⚙️ Stored WASM %s → CID: %s | Hash: %s\n", name, cid, hash)
		}
	}

	for name, info := range wasmIndex {
		if ZFSExists(info.CID) {
			Log("📤 Finalizing WASM %s → %s\n", name, info.CID)

			info.Assigned = true
			info.ClientIDs = []string{clientID}
			info.Chain = "civicL4"

			payload, _ := json.Marshal(info)
			indexEntry := fmt.Sprintf("wasm_index:%s:%s", name, string(payload))

			for _, chain := range chains {
				chain.Mint(ctx, Input{
					Type:      "wasm_index",
					Source:    clientID,
					Content:   indexEntry,
					Timestamp: time.Now().UTC(),
				})
			}
		}
	}

	for name, chain := range chains {
		Chains[name] = chain
	}

	Log("✅ ZEAM Chains Loaded.")

	return wasmIndex, shardIndex

}

func fetch(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		panic(Log("HTTP fetch failed: %v", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(Log("Body read failed: %v", err))
	}

	return string(body)
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
