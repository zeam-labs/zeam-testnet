package rewards

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/libp2p/go-libp2p/core/peer"
	"zeam/content"
)

type StorageTracker struct {
	mu sync.RWMutex

	addresses map[peer.ID]common.Address

	localPeerID peer.ID

	store      *content.Store
	providers  *content.ProviderRegistry
	epochs     *EpochManager
	challenges *ChallengeManager

	config *Config

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewStorageTracker(
	localPeerID peer.ID,
	store *content.Store,
	providers *content.ProviderRegistry,
	epochs *EpochManager,
	challenges *ChallengeManager,
	cfg *Config,
) *StorageTracker {
	ctx, cancel := context.WithCancel(context.Background())
	return &StorageTracker{
		addresses:   make(map[peer.ID]common.Address),
		localPeerID: localPeerID,
		store:       store,
		providers:   providers,
		epochs:      epochs,
		challenges:  challenges,
		config:      cfg,
		ctx:         ctx,
		cancel:      cancel,
	}
}

func (st *StorageTracker) RegisterAddress(peerID peer.ID, address common.Address) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.addresses[peerID] = address
	fmt.Printf("[Rewards] Registered %s -> %s\n", peerID.ShortString(), address.Hex()[:10])
}

func (st *StorageTracker) GetAddress(peerID peer.ID) (common.Address, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	addr, ok := st.addresses[peerID]
	return addr, ok
}

func (st *StorageTracker) Start() {

	if st.challenges != nil {
		st.challenges.OnChallengeComplete = func(record *ChallengeRecord) {
			st.epochs.RecordChallengeResult(record.Challenged, record.Passed)
			status := "PASSED"
			if !record.Passed {
				status = "FAILED"
			}
			fmt.Printf("[Rewards] Challenge %s: %s -> %s\n",
				status, record.Challenged.ShortString(), record.ChunkID.Hex()[:16])
		}
	}

	st.wg.Add(2)
	go st.trackLoop()
	go st.challengeLoop()

	fmt.Println("[Rewards] Storage tracker started")
}

func (st *StorageTracker) trackLoop() {
	defer st.wg.Done()

	st.updateStats()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-st.ctx.Done():
			return
		case <-ticker.C:
			st.updateStats()
		}
	}
}

func (st *StorageTracker) updateStats() {
	if st.providers == nil {
		return
	}

	st.mu.RLock()
	addresses := make(map[peer.ID]common.Address)
	for k, v := range st.addresses {
		addresses[k] = v
	}
	st.mu.RUnlock()

	allProviders := st.providers.All()

	type peerStats struct {
		bytes  uint64
		chunks int
	}
	stats := make(map[peer.ID]*peerStats)

	for _, records := range allProviders {
		for _, r := range records {
			s := stats[r.PeerID]
			if s == nil {
				s = &peerStats{}
				stats[r.PeerID] = s
			}
			s.bytes += r.Size
			s.chunks++
		}
	}

	for peerID, s := range stats {
		address, ok := addresses[peerID]
		if !ok {
			continue
		}
		st.epochs.UpdateContributor(peerID, address, s.bytes, s.chunks)
	}
}

func (st *StorageTracker) challengeLoop() {
	defer st.wg.Done()

	if st.challenges == nil {
		return
	}

	ticker := time.NewTicker(st.config.ChallengeFrequency)
	defer ticker.Stop()

	for {
		select {
		case <-st.ctx.Done():
			return
		case <-ticker.C:
			st.issueRandomChallenges()
		}
	}
}

func (st *StorageTracker) issueRandomChallenges() {
	if st.providers == nil || st.challenges == nil {
		return
	}

	allProviders := st.providers.All()
	if len(allProviders) == 0 {
		return
	}

	type chunkProvider struct {
		root     content.ContentID
		provider peer.ID
	}
	var targets []chunkProvider

	for root, records := range allProviders {

		if !st.store.Has(root) {
			continue
		}

		for _, r := range records {

			if r.PeerID == st.localPeerID {
				continue
			}
			targets = append(targets, chunkProvider{root: root, provider: r.PeerID})
		}
	}

	if len(targets) == 0 {
		return
	}

	numChallenges := min(3, len(targets))
	rand.Shuffle(len(targets), func(i, j int) {
		targets[i], targets[j] = targets[j], targets[i]
	})

	for i := 0; i < numChallenges; i++ {
		target := targets[i]
		ctx, cancel := context.WithTimeout(st.ctx, st.config.ChallengeTimeout)
		err := st.challenges.IssueChallenge(ctx, target.provider, target.root)
		cancel()

		if err != nil {
			fmt.Printf("[Rewards] Challenge to %s failed: %v\n", target.provider.ShortString(), err)
		}
	}
}

func (st *StorageTracker) GetStats() map[string]interface{} {
	st.mu.RLock()
	defer st.mu.RUnlock()

	return map[string]interface{}{
		"registered_addresses":   len(st.addresses),
		"current_epoch":          st.epochs.CurrentEpoch(),
		"accumulated_revenue":    st.epochs.GetAccumulatedRevenue().String(),
		"contributor_count":      st.epochs.ContributorCount(),
		"pending_challenges":     st.challenges.PendingCount(),
	}
}

func (st *StorageTracker) Stop() {
	st.cancel()
	st.wg.Wait()
	fmt.Println("[Rewards] Storage tracker stopped")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
