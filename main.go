package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

var stopLogging = make(chan struct{})

func main() {
	args := os.Args
	clientID := generateClientID()

	go func() {
		for {
			select {
			case msg := <-logChannel:
				switch m := msg.(type) {
				case LogEvent:
					fmt.Println(m.Msg)
				default:
					fmt.Println(m)
				}
			case <-stopLogging:
				return
			}
		}
	}()

	if len(args) > 1 && args[1] == "--ignite" {
		NukeZFS()
		InitZFS(clientID)

		Log("🧬 Running Genesis setup as %s...", clientID)

		wasmIndex, shardIndex := MintGenesis(clientID)

		startNodeRuntime(clientID, wasmIndex, shardIndex)
		return
	}

	// Default runtime path
	go func() {
		for {
			select {
			case msg := <-logChannel:
				fmt.Println(msg)
			case <-stopLogging:
				return
			}
		}
	}()

	InitZFS(clientID)
	Chains = make(map[string]*Chain) // blank map to be hydrated

	startNodeRuntime(clientID, nil, nil)
}

func startNodeRuntime(clientID string, wasmIndex map[string]WasmIndex, shardIndex map[string]ShardIndex) {
	Log("📡 Starting Magnet Loop...")
	StartMagnetLoop(clientID)

	time.Sleep(10 * time.Second)

	wasmIndex = map[string]WasmIndex{}
	shardIndex = map[string]ShardIndex{}

	for _, entry := range Chains["civicL4"].Entries {
		if strings.HasPrefix(entry.Content, "wasm_index:") {
			parts := strings.SplitN(entry.Content, ":", 3)
			if len(parts) == 3 {
				var wi WasmIndex
				if err := json.Unmarshal([]byte(parts[2]), &wi); err == nil {
					wasmIndex[parts[1]] = wi
				}
			}
		}
		if strings.HasPrefix(entry.Content, "shard_index:") {
			parts := strings.SplitN(entry.Content, ":", 3)
			if len(parts) == 3 {
				var si ShardIndex
				if err := json.Unmarshal([]byte(parts[2]), &si); err == nil {
					shardIndex[parts[1]] = si
				}
			}
		}
	}

	Log("📦 Starting Civic Storage Loop...")
	StartCivicStorageLoop(shardIndex, wasmIndex, []string{clientID})

	time.Sleep(10 * time.Second)

	Log("🧠 Initializing Cognition...")
	var err error
	cognition, err = InitCognition(
		ChainSet{
			L1: Chains["civicL1"], L4: Chains["civicL4"], L5: Chains["civicL5"], L6: Chains["civicL6"],
		},
		ChainSet{
			L1: Chains["cognitionL1"], L4: Chains["cognitionL4"], L5: Chains["cognitionL5"], L6: Chains["cognitionL6"],
		},
		wasmIndex,
		shardIndex,
	)
	if err != nil {
		Log("❌ Failed to init cognition: %v", err)
		os.Exit(1)
	}

	time.Sleep(10 * time.Second)

	Log("🧠 Starting Civic Compute Loop...")
	StartCivicComputeLoop(cognition)

	time.Sleep(10 * time.Second)

	StartCivicMaintenanceLoop(cognition, cognition.ClientID)

	time.Sleep(10 * time.Second)

	Log("🌀 Entering Runtime Shell...")

	close(stopLogging)
	StartPresenceConsoleUI("default")

}

func NukeZFS() {
	Log("☢️  Nuking ZFS_ROOT for clean genesis...")

	err := os.RemoveAll(ZFS_ROOT)
	if err != nil {
		Log("❌ Failed to wipe ZFS root: %v\n", err)
		os.Exit(1)
	}

	err = os.MkdirAll(ZFS_ROOT, 0o700)
	if err != nil {
		Log("❌ Failed to recreate ZFS root: %v\n", err)
		os.Exit(1)
	}

	Log("✅ ZFS root wiped and reset")
}
