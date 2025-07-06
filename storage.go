package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/disk"
)

const ZFS_ROOT = "/zeam/zfs"

var zfsDaemonOnce sync.Once
var zfsInitDone bool
var StorageState = map[string]*NodeStorageState{}

func StartCivicStorageLoop(
	shardIndex map[string]ShardIndex,
	wasmIndex map[string]WasmIndex,
	clientID []string,
) {
	Log("📦 Attempting to start CivicStorageLoop")
	zfsDaemonOnce.Do(func() {
		Log("✅ CivicStorageLoop started for client: %s", clientID)

		InitCivicStorageAssignments(shardIndex, wasmIndex, clientID)

		//go StartCivicStorageRepinnerLoop(shardIndex, wasmIndex, clientID)
	})
}

func InitCivicStorageAssignments(
	shardIndex map[string]ShardIndex,
	wasmIndex map[string]WasmIndex,
	clientID []string,
) {
	Log("📦 [ZFS] Performing initial shard assignment...")

	mintToL4 := func(ctx context.Context, input Input) {
		Chains["civicL4"].Mint(ctx, input)
		Chains["cognitionL4"].Mint(ctx, input)
	}

	maxBytes := GetAvailableStorageBytes(ZFS_ROOT) / 20

	AssignZFSStorageForWASM(wasmIndex, clientID, 3, maxBytes, mintToL4)
	AssignZFSStorageForShards(shardIndex, clientID, 3, maxBytes, mintToL4)

}

func StartCivicStorageRepinnerLoop(
	shardIndex map[string]ShardIndex,
	wasmIndex map[string]WasmIndex,
	clientIDs []string,
) {
	Log("🔍 [ZFS] Checking for missing pinned shards/modules...")

	for _, clientID := range clientIDs {
		node := StorageState[clientID]
		if node == nil {
			continue
		}

		for name, info := range shardIndex {
			if node.Assigned[info.CID] == "shard" && !ZFSExists(info.CID) {
				Log("📥 [ZFS] Re-pinning missing shard: %s (%s)", name, info.CID)
				data, err := fetchMeshShardData(info.CID)
				if err != nil {
					Log("❌ Failed to fetch shard %s: %v", name, err)
					continue
				}
				if err := ZFSWriteShard(info.CID, data); err != nil {
					Log("❌ Failed to write shard %s: %v", info.CID, err)
				} else {
					Log("📦 Re-pinned shard %s → %s", name, info.CID)
				}
			}
		}

		for name, info := range wasmIndex {
			if node.Assigned[info.CID] == "wasm" && !ZFSExists(info.CID) {
				Log("📥 [ZFS] Re-pinning missing wasm: %s (%s)", name, info.CID)
				data, err := fetchMeshShardData(info.CID)
				if err != nil {
					Log("❌ Failed to fetch wasm %s: %v", name, err)
					continue
				}
				if err := ZFSWriteShard(info.CID, data); err != nil {
					Log("❌ Failed to write wasm %s: %v", info.CID, err)
				} else {
					Log("📦 Re-pinned wasm %s → %s", name, info.CID)
				}
			}
		}
	}
}

func AssignZFSStorageForWASM(
	index map[string]WasmIndex,
	clientIDs []string,
	replicationFactor int,
	maxBytes uint64,
	mint MintFunc,
) []string {
	assigned := []string{}
	var names []string
	cidSizes := make(map[string]uint64)

	for name := range index {
		names = append(names, name)
	}
	sort.Strings(names)

	hashString := func(s string) string {
		h := sha256.Sum256([]byte(s))
		return hex.EncodeToString(h[:])
	}

	for _, clientID := range clientIDs {
		clientHash := hashString(clientID)
		var totalAssigned uint64

		for _, name := range names {
			sum := int(sha256.Sum256([]byte(clientHash + name))[0])
			if sum%replicationFactor != 0 {
				continue
			}

			cid := index[name].CID
			path := filepath.Join(ZFS_ROOT, cid)

			var size uint64
			if info, err := os.Stat(path); err == nil {
				size = uint64(info.Size())
			} else {
				size = 2 * 1024 * 1024 // fallback size
			}

			if totalAssigned+size > maxBytes {
				Log("🚫 Skipping wasm %s (%s) — would exceed node storage limit\n", name, cid)
				continue
			}

			totalAssigned += size
			cidSizes[cid] = size
			assigned = append(assigned, cid)

			if targetNode := StorageState[clientID]; targetNode != nil && targetNode.ClientID == clientID {
				if name != "" && !ZFSExists(cid) {
					Log("📥 Pinning wasm %s (%s)...\n", name, cid)
					data, err := fetchMeshShardData(cid)
					if err != nil {
						Log("❌ Failed to fetch wasm %s: %v\n", name, err)
					} else if err := ZFSWriteShard(cid, data); err != nil {
						Log("❌ Failed to write wasm %s: %v\n", cid, err)
					} else {
						Log("📦 Pinned wasm %s → %s\n", name, cid)
					}
				}
			}

			// ✅ Mint wasm_index with multiple clients
			wasmIndexEntry := WasmIndex{
				CID:       cid,
				ClientIDs: []string{clientID},
				Chain:     "civicL4",
				Assigned:  true,
			}
			indexPayload, _ := json.Marshal(wasmIndexEntry)
			indexLine := fmt.Sprintf("wasm_index:%s:%s", name, string(indexPayload))

			Chains["civicL4"].Mint(context.Background(), Input{
				Content:   indexLine,
				Source:    clientID,
				Timestamp: time.Now().UTC(),
			})
			Chains["cognitionL4"].Mint(context.Background(), Input{
				Content:   indexLine,
				Source:    clientID,
				Timestamp: time.Now().UTC(),
			})

			Log("📡 Assigned wasm %s (%s) — size %.2fMB → total used %.2fMB / %.2fMB\n",
				name, cid, float64(size)/1e6,
				float64(totalAssigned)/1e6, float64(maxBytes)/1e6)
		}

		if StorageState[clientID] == nil {
			StorageState[clientID] = &NodeStorageState{
				ClientID: clientID,
				Used:     0,
				Max:      maxBytes,
				Assigned: map[string]string{},
			}
		}

		for _, cid := range assigned {
			if _, already := StorageState[clientID].Assigned[cid]; !already {
				StorageState[clientID].Assigned[cid] = "wasm"
				StorageState[clientID].Used += cidSizes[cid]
			}
		}

		// 🧷 Final pass: ensure every CID is assigned to at least one node
		allCIDs := []string{}
		for _, wasm := range index {
			allCIDs = append(allCIDs, wasm.CID)
		}

		assigned := map[string]bool{}
		for _, node := range StorageState {
			for cid := range node.Assigned {
				assigned[cid] = true
			}
		}

		for _, cid := range allCIDs {
			if assigned[cid] {
				continue
			}

			// Pick lowest-usage node
			var target *NodeStorageState
			for _, node := range StorageState {
				if target == nil || node.Used < target.Used {
					target = node
				}
			}
			if target == nil {
				Log("❌ No available node for force-assign of %s", cid)
				continue
			}

			// Estimate size
			size := uint64(2 * 1024 * 1024)
			if info, err := os.Stat(filepath.Join(ZFS_ROOT, cid)); err == nil {
				size = uint64(info.Size())
			}

			target.Assigned[cid] = "wasm"
			target.Used += size

			// Mint assignment
			indexEntry := WasmIndex{
				CID:       cid,
				Chain:     "civicL4",
				ClientIDs: []string{target.ClientID},
				Assigned:  true,
			}
			payload, _ := json.Marshal(indexEntry)
			indexLine := fmt.Sprintf("wasm_index:%s", string(payload))

			Chains["civicL4"].Mint(context.Background(), Input{
				Source:    "zfs.force",
				Type:      "wasm_index",
				Content:   indexLine,
				Timestamp: time.Now().UTC(),
			})
			Chains["cognitionL4"].Mint(context.Background(), Input{
				Source:    "zfs.force",
				Type:      "wasm_index",
				Content:   indexLine,
				Timestamp: time.Now().UTC(),
			})

			Log("🧷 Force-assigned wasm %s to %s — now using %.2fMB / %.2fMB\n",
				cid, target.ClientID, float64(target.Used)/1e6, float64(target.Max)/1e6)

			// Pin if this node matches
			if ZFSExists(cid) {
				continue
			}
			if target.ClientID == clientID && !ZFSExists(cid) {
				Log("📥 Pinning forcibly assigned wasm %s...\n", cid)
				data, err := fetchMeshShardData(cid)
				if err != nil {
					Log("❌ Failed to fetch wasm %s: %v", cid, err)
					continue
				}
				if err := ZFSWriteShard(cid, data); err != nil {
					Log("❌ Failed to write wasm %s: %v", cid, err)
				} else {
					Log("📦 Force-pinned wasm %s to ZFS", cid)
				}
			}
		}

	}

	return assigned
}

func AssignZFSStorageForShards(
	index map[string]ShardIndex,
	clientIDs []string,
	replicationFactor int,
	maxBytes uint64,
	mint MintFunc,
) []string {
	assigned := []string{}
	var names []string
	cidSizes := make(map[string]uint64)

	for name := range index {
		names = append(names, name)
	}
	sort.Strings(names)

	hashString := func(s string) string {
		h := sha256.Sum256([]byte(s))
		return hex.EncodeToString(h[:])
	}

	for _, clientID := range clientIDs {
		clientHash := hashString(clientID)
		var totalAssigned uint64

		for _, name := range names {
			sum := int(sha256.Sum256([]byte(clientHash + name))[0])
			if sum%replicationFactor != 0 {
				continue
			}

			cid := index[name].CID
			path := filepath.Join(ZFS_ROOT, cid)

			var size uint64
			if info, err := os.Stat(path); err == nil {
				size = uint64(info.Size())
			} else {
				size = 2 * 1024 * 1024 // fallback size
			}

			if totalAssigned+size > maxBytes {
				Log("🚫 Skipping wasm %s (%s) — would exceed node storage limit\n", name, cid)
				continue
			}

			totalAssigned += size
			cidSizes[cid] = size
			assigned = append(assigned, cid)

			if targetNode := StorageState[clientID]; targetNode != nil && targetNode.ClientID == clientID {
				if name != "" && !ZFSExists(cid) {
					Log("📥 Pinning shard %s (%s)...\n", name, cid)
					data, err := fetchMeshShardData(cid)
					if err != nil {
						Log("❌ Failed to fetch shard %s: %v\n", name, err)
					} else if err := ZFSWriteShard(cid, data); err != nil {
						Log("❌ Failed to write shard %s: %v\n", cid, err)
					} else {
						Log("📦 Pinned shard %s → %s\n", name, cid)
					}
				}
			}

			// ✅ Mint wasm_index with multiple clients
			shardIndexEntry := ShardIndex{
				CID:       cid,
				ClientIDs: []string{clientID},
				Chain:     "civicL4",
				Assigned:  true,
			}
			indexPayload, _ := json.Marshal(shardIndexEntry)
			indexLine := fmt.Sprintf("shard_index:%s:%s", name, string(indexPayload))

			Chains["civicL4"].Mint(context.Background(), Input{
				Content:   indexLine,
				Source:    clientID,
				Timestamp: time.Now().UTC(),
			})
			Chains["cognitionL4"].Mint(context.Background(), Input{
				Content:   indexLine,
				Source:    clientID,
				Timestamp: time.Now().UTC(),
			})

			Log("📡 Assigned shard %s (%s) — size %.2fMB → total used %.2fMB / %.2fMB\n",
				name, cid, float64(size)/1e6,
				float64(totalAssigned)/1e6, float64(maxBytes)/1e6)
		}

		if StorageState[clientID] == nil {
			StorageState[clientID] = &NodeStorageState{
				ClientID: clientID,
				Used:     0,
				Max:      maxBytes,
				Assigned: map[string]string{},
			}
		}

		for _, cid := range assigned {
			if _, already := StorageState[clientID].Assigned[cid]; !already {
				StorageState[clientID].Assigned[cid] = "shard"
				StorageState[clientID].Used += cidSizes[cid]
			}
		}

		// 🧷 Final pass: ensure every CID is assigned to at least one node
		allCIDs := []string{}
		for _, shard := range index {
			allCIDs = append(allCIDs, shard.CID)
		}

		assigned := map[string]bool{}
		for _, node := range StorageState {
			for cid := range node.Assigned {
				assigned[cid] = true
			}
		}

		for _, cid := range allCIDs {
			if assigned[cid] {
				continue
			}

			// Pick lowest-usage node
			var target *NodeStorageState
			for _, node := range StorageState {
				if target == nil || node.Used < target.Used {
					target = node
				}
			}
			if target == nil {
				Log("❌ No available node for force-assign of %s", cid)
				continue
			}

			// Estimate size
			size := uint64(2 * 1024 * 1024)
			if info, err := os.Stat(filepath.Join(ZFS_ROOT, cid)); err == nil {
				size = uint64(info.Size())
			}

			target.Assigned[cid] = "shard"
			target.Used += size

			// Mint assignment
			indexEntry := ShardIndex{
				CID:       cid,
				Chain:     "civicL4",
				ClientIDs: []string{target.ClientID},
				Assigned:  true,
			}
			payload, _ := json.Marshal(indexEntry)
			indexLine := fmt.Sprintf("shard_index:%s", string(payload))

			Chains["civicL4"].Mint(context.Background(), Input{
				Source:    "zfs.force",
				Type:      "shard_index",
				Content:   indexLine,
				Timestamp: time.Now().UTC(),
			})
			Chains["cognitionL4"].Mint(context.Background(), Input{
				Source:    "zfs.force",
				Type:      "shard_index",
				Content:   indexLine,
				Timestamp: time.Now().UTC(),
			})

			Log("🧷 Force-assigned shard %s to %s — now using %.2fMB / %.2fMB\n",
				cid, target.ClientID, float64(target.Used)/1e6, float64(target.Max)/1e6)

			// Pin if this node matches
			if ZFSExists(cid) {
				continue
			}
			if target.ClientID == clientID && !ZFSExists(cid) {
				Log("📥 Pinning forcibly assigned shard %s...\n", cid)
				data, err := fetchMeshShardData(cid)
				if err != nil {
					Log("❌ Failed to fetch shard %s: %v", cid, err)
					continue
				}
				if err := ZFSWriteShard(cid, data); err != nil {
					Log("❌ Failed to write shard %s: %v", cid, err)
				} else {
					Log("📦 Force-pinned shard %s to ZFS", cid)
				}
			}
		}

	}

	return assigned
}

func InitZFS(clientID string) {
	if zfsInitDone {
		return
	}
	zfsInitDone = true

	if err := os.MkdirAll(ZFS_ROOT, 0o700); err != nil {
		Log("❌ Failed to initialize ZFS root: %v", err)
		os.Exit(1)
	}

	Log("🧱 ZFS civic storage daemon initialized at %v", ZFS_ROOT)
}

func ZFSPin(name string, data []byte) (string, error) {
	cid := hashBytes(data)
	path := filepath.Join(ZFS_ROOT, cid)

	// Encrypt before storage
	encrypted, err := EncryptWithPresenceKey(data)
	if err != nil {
		return "", fmt.Errorf("❌ encryption failed: %v", err)
	}

	if ZFSExists(cid) {
		existing, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("❌ failed to read existing file %s: %v", cid, err)
		}
		if !bytes.Equal(existing, encrypted) {
			return "", fmt.Errorf("❌ file hash match but encrypted content differs for %s", cid)
		}
		Log("📎 File %s already pinned (%s)\n", name, cid)
		return cid, nil
	}

	if err := os.WriteFile(path, encrypted, 0o600); err != nil {
		return "", fmt.Errorf("❌ write failed: %v", err)
	}

	// Mint store entry to all L4 chains
	for _, chain := range Chains {
		if chain.Layer == "L4" {
			chain.Mint(context.Background(), Input{
				Type:      "store",
				Source:    "zfs",
				Content:   fmt.Sprintf("store:cid:%s|name:%s", cid, name),
				Timestamp: time.Now().UTC(),
			})
		}
	}

	Log("📦 File pinned and minted → %s (%s)\n", name, cid)
	return cid, nil
}

func ZFSRead(cid string, chains ...*Chain) ([]byte, error) {
	Log("🌀 ZFS: Requesting CID %s from mesh...\n", cid)

	// Step 1: Attempt mesh resolution
	chunk, err := fetchMeshChunk(cid)
	if err != nil {
		return nil, fmt.Errorf("❌ ZFS mesh resolution failed for CID %s: %v", cid, err)
	}
	if len(chunk) == 0 {
		return nil, fmt.Errorf("❌ ZFS mesh returned empty chunk for CID %s", cid)
	}

	// Step 2: Decrypt with active Presence key
	plaintext, err := DecryptWithPresenceKey(chunk)
	if err != nil {
		return nil, fmt.Errorf("❌ ZFS decryption failed for CID %s: %v", cid, err)
	}

	// Step 3: Return plaintext
	return plaintext, nil
}

func ZFSExists(cid string) bool {
	path := filepath.Join(ZFS_ROOT, cid)
	_, err := os.Stat(path)
	return err == nil
}

func ZFSWriteShard(cid string, data []byte) error {
	path := filepath.Join(ZFS_ROOT, cid)
	return os.WriteFile(path, data, 0o600)
}

func ListAllMeshFiles() []MeshFile {
	var files []MeshFile
	seen := map[string]bool{}

	for _, chain := range Chains {
		for _, entry := range chain.Entries {
			if strings.HasPrefix(entry.Content, "store:cid:") {
				parts := strings.Split(entry.Content, "|")
				cid := strings.TrimPrefix(parts[0], "store:cid:")
				if !seen[cid] {
					files = append(files, MeshFile{CID: cid})
					seen[cid] = true
				}
			}
		}
	}
	return files
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func EncryptWithPresenceKey(data []byte) ([]byte, error) {
	// TODO: Implement real encryption
	return data, nil
}

func DecryptWithPresenceKey(encrypted []byte) ([]byte, error) {
	// TODO: Implement real decryption
	return encrypted, nil
}

func GetAvailableStorageBytes(path string) uint64 {
	usage, err := disk.Usage(path)
	if err != nil {
		return 0
	}
	return usage.Free
}

func fetchMeshChunk(cid string) ([]byte, error) {
	Log("📡 [ZFS] Attempting to resolve CID %s from mesh...\n", cid)

	// Step 1: Ask the civic mesh for the known list of peers who *might* hold this shard
	peers := DiscoverDNSBootstrapPeers("public.zeam.foundation")

	// Step 2: Try every peer until one returns non-empty data
	for _, peer := range peers {
		host := strings.Split(peer, ":")[0]
		address := fmt.Sprintf("%s:9031", host)

		conn, err := net.Dial("tcp", address)
		if err != nil {
			Log("⚠️  Mesh connect failed to %s: %v\n", address, err)
			continue
		}

		// Build request
		req := map[string]string{
			"type": "zfs_request",
			"cid":  cid,
		}
		b, _ := json.Marshal(req)
		conn.Write(b)

		// Attempt to read full shard into memory
		data, err := io.ReadAll(conn)
		conn.Close()

		if err != nil {
			Log("⚠️  Failed to read shard %s from %s: %v\n", cid, address, err)
			continue
		}
		if len(data) == 0 {
			Log("⚠️  Empty shard %s from %s\n", cid, address)
			continue
		}

		Log("✅ Shard %s received from %s (%d bytes)\n", cid, address, len(data))
		return data, nil
	}

	// All peers failed
	return nil, fmt.Errorf("❌ no mesh peer responded with shard %s", cid)
}

func fetchMeshShardData(cid string) ([]byte, error) {
	peers := DiscoverDNSBootstrapPeers("public.zeam.foundation")
	for _, peer := range peers {
		host := strings.Split(peer, ":")[0]
		address := fmt.Sprintf("%s:9031", host)

		conn, err := net.Dial("tcp", address)
		if err != nil {
			continue
		}

		req := map[string]string{
			"type": "zfs_request",
			"cid":  cid,
		}
		b, _ := json.Marshal(req)
		conn.Write(b)

		// Attempt to read the full shard into memory
		data, err := io.ReadAll(conn)
		conn.Close()
		if err != nil || len(data) == 0 {
			continue
		}

		Log("📦 Mesh shard %s received (%d bytes) from %s\n", cid, len(data), peer)
		return data, nil
	}

	return nil, fmt.Errorf("no mesh peer responded with shard %s", cid)
}

func InitializeStorageNodes(clientIDs []string) {
	for _, id := range clientIDs {
		if _, exists := StorageState[id]; exists {
			Log("⚠️  Skipping reinit of node %s — already exists in StorageState", id)
			continue
		}

		maxBytes := GetAvailableStorageBytes(ZFS_ROOT)
		if maxBytes == 0 {
			maxBytes = 1 << 30 // fallback to 1GB if storage check fails
		}

		StorageState[id] = &NodeStorageState{
			ClientID: id,
			Used:     0,
			Max:      maxBytes,
			Assigned: map[string]string{},
		}

		Log("🧱 Initialized node %s — Max capacity: %.2f GB", id, float64(maxBytes)/1e9)
	}
}
