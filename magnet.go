package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
)

var BlockHeight int = 0
var LastBlockHash string = ""
var MeshSynced bool = false
var PeerSynced = make(map[string]bool)

func StartMagnetLoop(clientID string) {
	Log("🧲 Magnet loop initializing...")

	// Start TCP listener for incoming sync
	go StartGossipNetListener(9031)
	Log("🧲 GossipNet TCP listener launched on :9031")

	// Optional one-shot passive DNS awareness
	go func() {
		time.Sleep(3 * time.Second) // allow listener to settle

		dnsPeers := DiscoverDNSBootstrapPeers("public.zeam.foundation")
		if len(dnsPeers) == 0 {
			Log("⚠️  No DNS bootstrap peers found")
			return
		}

		selfIP := getSelfIP()
		anyConnected := false

		for _, peer := range dnsPeers {
			host := strings.Split(peer, ":")[0]
			if host == selfIP {
				Log("🔁 Skipping self (%s) in DNS peer list\n", peer)
				continue
			}

			Log("🌐 Attempting initial sync with peer: %s\n", peer)
			go StartGossipSyncClient(peer, clientID)
			anyConnected = true
		}

		if !anyConnected {
			Log("🌐 No external peers accepted connection — waiting for inbound mesh pull...")
		}
	}()

	Log("✅ Magnet loop fully operational")
}

func StartGossipSyncClient(peerAddr string, clientID string) {
	go func() {
		Log("🌐 Connecting to mesh peer: %s\n", peerAddr)
		conn, err := net.Dial("tcp", peerAddr)
		if err != nil {
			Log("❌ GossipNet dial failed: %s\n", err)
			return
		}
		defer conn.Close()

		// Ensure local StorageState exists before accessing
		if StorageState[clientID] == nil {
			StorageState[clientID] = &NodeStorageState{
				ClientID: clientID,
				Assigned: map[string]string{},
			}
		}

		// CPU stats
		usage, _ := cpu.Percent(0, false)
		cpuUsage := 0.0
		cpuFree := 0.0
		if len(usage) > 0 {
			cpuUsage = math.Round(usage[0]*100) / 100
			cpuFree = math.Round((100.0-usage[0])*100) / 100
		}

		assigned := len(StorageState[clientID].Assigned)

		// Build handshake
		handshake := map[string]interface{}{
			"type":           "hello",
			"clientID":       clientID,
			"timestamp":      time.Now().UTC().Format(time.RFC3339),
			"used_bytes":     StorageState[clientID].Used,
			"max_bytes":      StorageState[clientID].Max,
			"cpu_usage_pct":  cpuUsage,
			"cpu_free_pct":   cpuFree,
			"assigned_count": assigned,
		}

		b, _ := json.Marshal(handshake)
		conn.Write(b)

		// Read response (chains, if any)
		buf := new(bytes.Buffer)
		_, err = buf.ReadFrom(conn)
		if err != nil {
			Log("❌ Failed to read from peer: %s\n", err)
			return
		}

		payload := buf.String()
		if strings.TrimSpace(payload) == "" {
			Log("🛑 No response from peer — silent mode confirmed")
			return
		}

		parts := strings.Split(payload, "---END-CHAIN---")
		count := 0

		for _, raw := range parts {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}

			var chain Chain
			if err := json.Unmarshal([]byte(raw), &chain); err != nil {
				Log("❌ Failed to parse chain: %v", err)
				continue
			}

			if chain.Name == "" {
				Log("⚠️  Received unnamed chain — skipping")
				continue
			}

			existing, exists := Chains[chain.Name]
			if !exists {
				Chains[chain.Name] = &Chain{
					Name:    chain.Name,
					Entries: dedupeEntries(chain.Entries, nil),
				}
			} else {
				Chains[chain.Name].Entries = dedupeEntries(chain.Entries, existing.Entries)
			}

			Log("🔗 Imported chain %s with %d entries\n", chain.Name, len(chain.Entries))
			count++
		}

		if count == 0 {
			Log("🧘 Peer %s chose not to share chain data", peerAddr)
		} else {
			Log("✅ Mesh sync complete. %d chains hydrated.\n", count)
			MeshSynced = true
		}
	}()
}

func StartGossipNetListener(port int) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		Log("❌ Failed to start GossipNet listener: %v\n", err)
		return
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				continue
			}
			go handleGossipConnection(conn)
		}
	}()
}

func handleGossipConnection(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}
	msg := strings.TrimSpace(string(buf[:n]))

	// Check for a zfs_request first
	if strings.Contains(msg, `"type":"zfs_request"`) {
		var req struct {
			Type string `json:"type"`
			CID  string `json:"cid"`
		}
		if err := json.Unmarshal(buf[:n], &req); err != nil {
			Log("❌ Invalid zfs_request format")
			return
		}

		path := filepath.Join(ZFS_ROOT, req.CID)
		data, err := os.ReadFile(path)
		if err != nil {
			Log("❌ ZFS: requested shard %s not found\n", req.CID)
			return
		}

		// Stream back raw data
		conn.Write(data)
		Log("📤 Served shard %s (%d bytes)\n", req.CID, len(data))
		return
	}

	// Default: handshake and chain sync
	if strings.Contains(msg, `"type":"hello"`) {
		var hello map[string]interface{}
		if err := json.Unmarshal(buf[:n], &hello); err == nil {
			clientID := hello["clientID"].(string)

			used := uint64(hello["used_bytes"].(float64))
			max := uint64(hello["max_bytes"].(float64))
			cpuUsage := hello["cpu_usage_pct"].(float64)
			cpuFree := hello["cpu_free_pct"].(float64)
			assigned := int(hello["assigned_count"].(float64))

			if StorageState[clientID] == nil {
				StorageState[clientID] = &NodeStorageState{
					ClientID: clientID,
					Assigned: map[string]string{},
				}
			}

			node := StorageState[clientID]
			node.Used = used
			node.Max = max

			Log("📡 Mesh sync: %s → %.2fGB / %.2fGB | CPU: %.1f%% used, %.1f%% free, %d assigned",
				clientID, float64(used)/1e9, float64(max)/1e9, cpuUsage, cpuFree, assigned)
		}

		Log("🤝 Handshake received — sending chains")
	}

	Log("🧠 Sending %d chains...\n", len(Chains))
	for _, chain := range Chains {
		raw, _ := json.Marshal(chain)
		conn.Write(raw)
		conn.Write([]byte("\n---END-CHAIN---\n"))
	}
}

func dedupeEntries(incoming, existing []Input) []Input {
	existingMap := map[string]bool{}
	for _, e := range existing {
		key := e.Timestamp.String() + "|" + e.Source + "|" + e.Content
		existingMap[key] = true
	}

	var result []Input
	result = append(result, existing...) // preserve existing

	for _, e := range incoming {
		key := e.Timestamp.String() + "|" + e.Source + "|" + e.Content
		if !existingMap[key] {
			result = append(result, e)
		}
	}
	return result
}

func DiscoverDNSBootstrapPeers(domain string) []string {
	ips, err := net.LookupIP(domain)
	if err != nil {
		Log("❌ DNS lookup failed for %s: %v\n", domain, err)
		return nil
	}
	var peers []string
	for _, ip := range ips {
		peers = append(peers, fmt.Sprintf("%s:9031", ip.String()))
	}
	return peers
}

func getSelfIP() string {
	con, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer con.Close()
	localAddr := con.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
