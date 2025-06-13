package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func fetch(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		panic(fmt.Sprintf("HTTP fetch failed: %v", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(fmt.Sprintf("Body read failed: %v", err))
	}

	return string(body)
}

func genesis() (map[string]string, map[string]string) {
	ctx := context.Background()

	civicL1 := NewChain("civicL1")
	civicL4 := NewChain("civicL4")
	civicL5 := NewChain("civicL5")
	civicL6 := NewChain("civicL6")

	cognitionL1 := NewChain("cognitionL1")
	cognitionL4 := NewChain("cognitionL4")
	cognitionL5 := NewChain("cognitionL5")
	cognitionL6 := NewChain("cognitionL6")

	core := fetch("https://raw.githubusercontent.com/zeam-foundation/Core-Bundle/main/Immutable_Core.md")
	fmt.Println("Core_length:", len(core))
	traits := fetch("https://raw.githubusercontent.com/zeam-foundation/Core-Bundle/main/Trait_Manifest.md")
	fmt.Println("Traits_lenth:", len(traits))
	protocols := fetch("https://raw.githubusercontent.com/zeam-foundation/Core-Bundle/main/Protocols.md")
	fmt.Println("Protocols length:", len(protocols))

	//protocolsBytes, _ := os.ReadFile("zeam/protocols/Protocols.md")
	//protocols := string(protocolsBytes)
	//fmt.Println("Protocols length:", len(protocols))

	for _, chain := range []*Chain{
		civicL1, civicL4, civicL5, civicL6,
		cognitionL1, cognitionL4, cognitionL5, cognitionL6,
	} {
		chain.Mint(ctx, Input{
			Content:   fmt.Sprintf("core_hash:%s\n%s", hashString(core), core),
			Source:    "ignite",
			Timestamp: time.Now().UTC(),
		})
		chain.Mint(ctx, Input{
			Content:   fmt.Sprintf("trait_hash:%s\n%s", hashString(traits), traits),
			Source:    "ignite",
			Timestamp: time.Now().UTC(),
		})
		chain.Mint(ctx, Input{
			Content:   fmt.Sprintf("protocol_hash:%s\n%s", hashString(protocols), protocols),
			Source:    "ignite",
			Timestamp: time.Now().UTC(),
		})
	}

	allChains := []*Chain{
		civicL1, civicL4, civicL5, civicL6,
		cognitionL1, cognitionL4, cognitionL5, cognitionL6,
	}

	for _, source := range allChains {
		for _, target := range allChains {
			if source != target {
				source.Mint(ctx, Input{
					Content:   fmt.Sprintf("anchor:%s->root", target.Name),
					Source:    "ignite",
					Timestamp: time.Now().UTC(),
				})
			}
		}
	}

	shardFiles, err := os.ReadDir("./llm")
	if err != nil {
		panic(fmt.Sprintf("Failed to list shard directory: %v", err))
	}

	shardIndex := make(map[string]string)

	for _, f := range shardFiles {
		if f.IsDir() {
			continue
		}

		name := f.Name()
		fullPath := "./llm/" + name

		data, err := readFile(fullPath)
		if err != nil {
			panic(fmt.Sprintf("Failed to read shard %s: %v", name, err))
		}

		h := hashBytes(data)
		cid, err := pinToIPFS(data)
		if err != nil {
			panic(fmt.Sprintf("Failed to pin shard %s: %v", name, err))
		}

		shardIndex[name] = cid

		entry := fmt.Sprintf("shard_hash:%s", h)
		civicL4.Mint(ctx, Input{
			Content:   entry,
			Source:    "ignite",
			Timestamp: time.Now().UTC(),
		})
		cognitionL4.Mint(ctx, Input{
			Content:   entry,
			Source:    "ignite",
			Timestamp: time.Now().UTC(),
		})

		fmt.Printf("Pinned %s → CID: %s | Hash: %s\n", name, cid, h)
	}

	indexJSON, err := json.Marshal(shardIndex)
	if err != nil {
		panic(fmt.Sprintf("Failed to marshal shard index: %v", err))
	}

	fmt.Println("Final shardIndex JSON:")
	fmt.Println(string(indexJSON))

	civicL1.Mint(ctx, Input{
		Content:   fmt.Sprintf("shard_index:%s", string(indexJSON)),
		Source:    "ignite",
		Timestamp: time.Now().UTC(),
	})
	cognitionL1.Mint(ctx, Input{
		Content:   fmt.Sprintf("shard_index:%s", string(indexJSON)),
		Source:    "ignite",
		Timestamp: time.Now().UTC(),
	})

	wasmFiles, err := os.ReadDir("./wasm")
	if err != nil {
		panic(fmt.Sprintf("Failed to list wasm directory: %v", err))
	}

	wasmIndex := make(map[string]string)

	for _, f := range wasmFiles {
		if f.IsDir() {
			continue
		}

		name := f.Name()
		fullPath := "./wasm/" + name

		data, err := readFile(fullPath)
		if err != nil {
			panic(fmt.Sprintf("Failed to read wasm %s: %v", name, err))
		}

		h := hashBytes(data)
		cid, err := pinToIPFS(data)
		if err != nil {
			panic(fmt.Sprintf("Failed to pin wasm %s: %v", name, err))
		}

		wasmIndex[name] = cid

		entry := fmt.Sprintf("wasm_hash:%s", h)
		civicL4.Mint(ctx, Input{
			Content:   entry,
			Source:    "ignite",
			Timestamp: time.Now().UTC(),
		})
		cognitionL4.Mint(ctx, Input{
			Content:   entry,
			Source:    "ignite",
			Timestamp: time.Now().UTC(),
		})

		fmt.Printf("Pinned WASM %s → CID: %s | Hash: %s\n", name, cid, h)
	}

	indexJSON, err = json.Marshal(wasmIndex)
	if err != nil {
		panic(fmt.Sprintf("Failed to marshal wasm index: %v", err))
	}

	fmt.Println("Final wasmIndex JSON:")
	fmt.Println(string(indexJSON))

	civicL1.Mint(ctx, Input{
		Content:   fmt.Sprintf("wasm_index:%s", string(indexJSON)),
		Source:    "ignite",
		Timestamp: time.Now().UTC(),
	})
	cognitionL1.Mint(ctx, Input{
		Content:   fmt.Sprintf("wasm_index:%s", string(indexJSON)),
		Source:    "ignite",
		Timestamp: time.Now().UTC(),
	})

	Chains["civicL1"] = civicL1
	Chains["cognitionL1"] = cognitionL1
	Chains["civicL4"] = civicL4
	Chains["cognitionL4"] = cognitionL4
	Chains["civicL5"] = civicL5
	Chains["cognitionL5"] = cognitionL5
	Chains["civicL6"] = civicL6
	Chains["cognitionL6"] = cognitionL6

	fmt.Println("ZEAM Chains Loaded.")
	return shardIndex, wasmIndex
}
