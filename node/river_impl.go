package node

import (
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type NetworkFeedConfig struct {
	Hybrid *HybridTransport
	Gossip *OpNodeGossip
}

type NetworkFeedImpl struct {
	hybrid *HybridTransport
	gossip *OpNodeGossip

	fanOut *EventFanOut

	mempoolRate  float64
	lastTxHash   time.Time
	txCount      uint64
	txCountStart time.Time
	metricsMu    sync.RWMutex

	stopCh  chan struct{}
	stopped bool
	mu      sync.Mutex
}

func NewNetworkFeed(cfg *NetworkFeedConfig) *NetworkFeedImpl {
	nf := &NetworkFeedImpl{
		hybrid:       cfg.Hybrid,
		gossip:       cfg.Gossip,
		fanOut:       newEventFanOut(),
		stopCh:       make(chan struct{}),
		txCountStart: time.Now(),
	}

	nf.hybrid.OnTxHashReceived(func(hashes []common.Hash) {
		nf.recordTxHashes(len(hashes))
		nf.fanOut.Emit(NetworkEvent{
			Type:      EventTxHashes,
			ChainID:   nf.hybrid.config.NetworkID,
			Timestamp: time.Now(),
			Hashes:    hashes,
		})
	})

	nf.hybrid.OnTxReceived(func(txs []*types.Transaction) {
		nf.fanOut.Emit(NetworkEvent{
			Type:      EventTxFull,
			ChainID:   nf.hybrid.config.NetworkID,
			Timestamp: time.Now(),
			Txs:       txs,
		})
	})

	nf.hybrid.OnZEAMMessage(func(tx *types.Transaction, data []byte) {
		nf.fanOut.Emit(NetworkEvent{
			Type:      EventZEAMMessage,
			ChainID:   nf.hybrid.config.NetworkID,
			Timestamp: time.Now(),
			ZEAMData:  data,
			ZEAMTx:    tx,
		})
	})

	nf.hybrid.OnBlockHashReceived(func(hash common.Hash, number uint64) {
		nf.fanOut.Emit(NetworkEvent{
			Type:          EventL1Block,
			ChainID:       nf.hybrid.config.NetworkID,
			Timestamp:     time.Now(),
			L1BlockHash:   hash,
			L1BlockNumber: number,
		})
	})

	nf.hybrid.OnPeerConnect(func(peerID string) {
		nf.fanOut.Emit(NetworkEvent{
			Type:      EventPeerConnect,
			ChainID:   nf.hybrid.config.NetworkID,
			Timestamp: time.Now(),
			PeerID:    peerID,
		})
	})

	nf.hybrid.OnPeerDrop(func(peerID string) {
		nf.fanOut.Emit(NetworkEvent{
			Type:      EventPeerDrop,
			ChainID:   nf.hybrid.config.NetworkID,
			Timestamp: time.Now(),
			PeerID:    peerID,
		})
	})

	if nf.gossip != nil {
		nf.gossip.OnBlockReceived = func(chainID uint64, block *L2BlockGossip) {
			nf.fanOut.Emit(NetworkEvent{
				Type:      EventL2Block,
				ChainID:   chainID,
				Timestamp: time.Now(),
				L2Block:   block,
			})

			if block != nil {
				txHashes := block.ExtractTransactionHashes()
				if len(txHashes) > 0 {
					nf.recordTxHashes(len(txHashes))
					nf.fanOut.Emit(NetworkEvent{
						Type:      EventTxHashes,
						ChainID:   chainID,
						Timestamp: time.Now(),
						Hashes:    txHashes,
					})
				}
			}
		}

		nf.gossip.OnPeerJoin = func(chainID uint64, peerID string) {
			nf.fanOut.Emit(NetworkEvent{
				Type:      EventPeerConnect,
				ChainID:   chainID,
				Timestamp: time.Now(),
				PeerID:    peerID,
			})
		}

		nf.gossip.OnPeerLeave = func(chainID uint64, peerID string) {
			nf.fanOut.Emit(NetworkEvent{
				Type:      EventPeerDrop,
				ChainID:   chainID,
				Timestamp: time.Now(),
				PeerID:    peerID,
			})
		}
	}

	return nf
}

func (nf *NetworkFeedImpl) recordTxHashes(count int) {
	nf.metricsMu.Lock()
	defer nf.metricsMu.Unlock()
	nf.txCount += uint64(count)
	nf.lastTxHash = time.Now()
	elapsed := time.Since(nf.txCountStart).Seconds()
	if elapsed > 0 {
		nf.mempoolRate = float64(nf.txCount) / elapsed
	}
}

func (nf *NetworkFeedImpl) Subscribe(bufSize int) *FeedSubscription {
	return nf.fanOut.Subscribe(bufSize)
}

func (nf *NetworkFeedImpl) Broadcast(payload []byte) <-chan BroadcastResult {
	ch := make(chan BroadcastResult, 1)
	go func() {
		hash, err := nf.hybrid.BroadcastZEAM(payload)
		ch <- BroadcastResult{TxHash: hash, Err: err}
		close(ch)
	}()
	return ch
}

func (nf *NetworkFeedImpl) BroadcastTx(tx *types.Transaction) <-chan BroadcastResult {
	ch := make(chan BroadcastResult, 1)
	go func() {
		err := nf.hybrid.Broadcast(tx)
		var hash common.Hash
		if tx != nil {
			hash = tx.Hash()
		}
		ch <- BroadcastResult{TxHash: hash, Err: err}
		close(ch)
	}()
	return ch
}

func (nf *NetworkFeedImpl) Stats() NetworkStats {
	nf.metricsMu.RLock()
	rate := nf.mempoolRate
	lastTx := nf.lastTxHash
	nf.metricsMu.RUnlock()

	relays, direct := nf.hybrid.Stats()

	stats := NetworkStats{
		L1Peers:     nf.hybrid.PeerCount(),
		L2Peers:     make(map[uint64]int),
		MempoolRate: rate,
		LastTxHash:  lastTx,
		RelayAddrs:  relays,
		DirectConns: direct,
	}

	if nf.gossip != nil {
		gossipStats := nf.gossip.GetStats()
		for chainID, chainStats := range gossipStats {
			if peerCount, ok := chainStats["peers"]; ok {
				if pc, ok := peerCount.(int); ok {
					stats.L2Peers[chainID] = pc
				}
			}
		}
	}

	return stats
}

func (nf *NetworkFeedImpl) Start() error {
	if err := nf.hybrid.Start(); err != nil {
		return fmt.Errorf("failed to start hybrid transport: %w", err)
	}

	if nf.gossip != nil {
		if err := nf.gossip.Start(); err != nil {
			nf.hybrid.Stop()
			return fmt.Errorf("failed to start L2 gossip: %w", err)
		}
	}

	fmt.Println("[NetworkFeed] Started")
	return nil
}

func (nf *NetworkFeedImpl) Stop() {
	nf.mu.Lock()
	defer nf.mu.Unlock()
	if nf.stopped {
		return
	}
	nf.stopped = true
	close(nf.stopCh)

	if nf.gossip != nil {
		nf.gossip.Stop()
	}
	nf.hybrid.Stop()
	nf.fanOut.Close()

	fmt.Println("[NetworkFeed] Stopped")
}
