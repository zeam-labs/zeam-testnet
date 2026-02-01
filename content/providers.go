package content

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type ProviderRecord struct {
	PeerID    peer.ID
	Root      ContentID
	Size      uint64
	Announced time.Time
}

type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[ContentID][]ProviderRecord
	ttl       time.Duration
}

func NewProviderRegistry(ttl time.Duration) *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[ContentID][]ProviderRecord),
		ttl:       ttl,
	}
}

func (pr *ProviderRegistry) Add(root ContentID, peerID peer.ID, size uint64) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	records := pr.providers[root]

	for i, r := range records {
		if r.PeerID == peerID {
			records[i].Announced = time.Now()
			records[i].Size = size
			return
		}
	}
	pr.providers[root] = append(records, ProviderRecord{
		PeerID:    peerID,
		Root:      root,
		Size:      size,
		Announced: time.Now(),
	})
}

func (pr *ProviderRegistry) Get(root ContentID) []ProviderRecord {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	records := pr.providers[root]
	out := make([]ProviderRecord, 0, len(records))
	now := time.Now()
	for _, r := range records {
		if now.Sub(r.Announced) < pr.ttl {
			out = append(out, r)
		}
	}
	return out
}

func (pr *ProviderRegistry) Remove(root ContentID, peerID peer.ID) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	records := pr.providers[root]
	for i, r := range records {
		if r.PeerID == peerID {
			pr.providers[root] = append(records[:i], records[i+1:]...)
			return
		}
	}
}

func (pr *ProviderRegistry) Prune() {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	now := time.Now()
	for root, records := range pr.providers {
		var valid []ProviderRecord
		for _, r := range records {
			if now.Sub(r.Announced) < pr.ttl {
				valid = append(valid, r)
			}
		}
		if len(valid) == 0 {
			delete(pr.providers, root)
		} else {
			pr.providers[root] = valid
		}
	}
}

func (pr *ProviderRegistry) All() map[ContentID][]ProviderRecord {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	now := time.Now()
	result := make(map[ContentID][]ProviderRecord)
	for root, records := range pr.providers {
		var valid []ProviderRecord
		for _, r := range records {
			if now.Sub(r.Announced) < pr.ttl {
				valid = append(valid, r)
			}
		}
		if len(valid) > 0 {
			result[root] = valid
		}
	}
	return result
}

func (pr *ProviderRegistry) Count() int {
	return len(pr.All())
}
