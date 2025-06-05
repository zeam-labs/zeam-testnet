// ignite.go
package main

import (
        "context"
        "crypto/sha256"
        "encoding/hex"
        "fmt"
        "io"
        "io/ioutil"
        "log"
        "net/http"
        "os"

        "zeam/civic"
        "zeam/cognition"
        "zeam/ipfs"
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

func hash(data []byte) string {
        h := sha256.Sum256(data)
        return hex.EncodeToString(h[:])
}

func read(path string) []byte {
        b, err := os.ReadFile(path)
        if err != nil {
                panic(fmt.Sprintf("failed to read %s: %v", path, err))
        }
        return b
}

func main() {
	ctx := context.Background()

    // Initialize IPFS
    err := ipfs.InitIPFS("localhost:5001")
    if err != nil {
            log.Fatal("IPFS not available:", err)
    }

	// Create 8 separate chains across two meshes
	civicL1 := app.NewChain("civic-L1")
	civicL4 := app.NewChain("civic-L4")
	civicL5 := app.NewChain("civic-L5")
	civicL6 := app.NewChain("civic-L6")

	cognitionL1 := app.NewChain("cognition-L1")
	cognitionL4 := app.NewChain("cognition-L4")
	cognitionL5 := app.NewChain("cognition-L5")
	cognitionL6 := app.NewChain("cognition-L6")

	// Fetch sealed documents
	core := fetch("https://raw.githubusercontent.com/zeam-foundation/Core-Bundle/main/Immutable_Core.md")
	traits := fetch("https://raw.githubusercontent.com/zeam-foundation/Core-Bundle/main/Trait_Manifest.md")
	protocols := fetch("https://raw.githubusercontent.com/zeam-foundation/Core-Bundle/main/Protocols.md")

	// Mint L1 content to both blockmeshes
	hash := func(data string) string {
		return fmt.Sprintf("%x", sha256.Sum256([]byte(data)))
	}

	for _, chain := range []*app.App{civicL1, cognitionL1} {
		chain.Mint(ctx, fmt.Sprintf("core_hash:%s\n%s", hash(core), core))
		chain.Mint(ctx, fmt.Sprintf("trait_hash:%s\n%s", hash(traits), traits))
		chain.Mint(ctx, fmt.Sprintf("protocol_hash:%s\n%s", hash(protocols), protocols))
	}

	// Mint root anchors for L4–L6 on both meshes
	for _, chain := range []*app.App{civicL4, civicL5, civicL6, cognitionL4, cognitionL5, cognitionL6} {
		chain.Mint(ctx, "anchor:root")
	}

	// Cross-anchor meshes
	civicL1.Mint(ctx, fmt.Sprintf("anchor:cognition-L1->%s", hash(core)))
	cognitionL1.Mint(ctx, fmt.Sprintf("anchor:civic-L1->%s", hash(core)))

	civicL4.Mint(ctx, "anchor:cognition-L4->root")
	cognitionL4.Mint(ctx, "anchor:civic-L4->root")

	civicL5.Mint(ctx, "anchor:cognition-L5->root")
	cognitionL5.Mint(ctx, "anchor:civic-L5->root")

	civicL6.Mint(ctx, "anchor:cognition-L6->root")
	cognitionL6.Mint(ctx, "anchor:civic-L6->root")

	civicL6.Mint(ctx, "anchor:civic-L1->root")
	civicL6.Mint(ctx, "anchor:civic-L4->root")
	civicL6.Mint(ctx, "anchor:civic-L5->root")

	cognitionL6.Mint(ctx, "anchor:cognition-L1->root")
	cognitionL6.Mint(ctx, "anchor:cognition-L4->root")
	cognitionL6.Mint(ctx, "anchor:cognition-L5->root")

	// 5. Pin LLM shard binaries via IPFS, anchor hash only
    shardFiles, err := ioutil.ReadDir("./llm/shards")
    if err != nil {
            log.Fatalf("Failed to list shard directory: %v", err)
    }

    for _, f := range shardFiles {
            fullPath := "./llm/shards/" + f.Name()

            cid, err := ipfs.PinFile(fullPath)
            if err != nil {
                    log.Fatalf("Failed to pin shard %s: %v", f.Name(), err)
            }

            data := read(fullPath)
            h := hash(data)

            if !ipfs.VerifyShard(data, h) {
                    log.Fatalf("Shard verification failed for %s", f.Name())
            }

            entry := fmt.Sprintf("shard_hash:%s", h)
            civicL4.Mint(ctx, entry)
            cognitionL4.Mint(ctx, entry)

            fmt.Printf("Pinned %s → CID: %s | Hash: %s\n", f.Name(), cid, h)
    }

    fmt.Println("ZEAM ignition complete.")
}
