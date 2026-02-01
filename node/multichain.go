package node

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/libp2p/go-libp2p/core/host"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

type MultiChainTransport struct {
	mu sync.RWMutex

	sharedLibP2P *LibP2PTransport

	chains map[uint64]*ChainTransport

	opNodeGossip *OpNodeGossip

	unifiedFlow *MempoolFlow

	totalTxSeen   uint64
	totalPeerSeen uint64
	totalL2Blocks uint64

	OnTxReceived   func(chainID uint64, hashes []common.Hash)
	OnTxParsed     func(chainID uint64, tx *types.Transaction)
	OnPeerEvent    func(chainID uint64, peerID string, connected bool)
	OnBlockAdvance func(chainID uint64, blockNum uint64)
	OnL2Block      func(chainID uint64, block *L2BlockGossip)
	OnPriceGossip  func(update *PriceUpdate)

	priceAggregator  *PriceAggregator
	priceBroadcaster *PriceBroadcaster

	config *MultiChainConfig

	feed *multiChainFeed

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup

	statsLogCount uint64
}

type ChainTransport struct {
	ChainID   uint64
	Name      string
	Config    *ChainConfig
	Hybrid    *HybridTransport
	IsL2      bool

	CurrentBlock  uint64
	LastBlockTime time.Time
	PeerCount     int32

	trackedTxs map[common.Hash]time.Time
	mu         sync.RWMutex

	txCount    uint64
	swapCount  uint64
	blockCount uint64
}

type MultiChainConfig struct {
	PrivateKey    *ecdsa.PrivateKey
	EnabledChains []uint64
	ListenPorts   map[uint64]string
	RPCEndpoints  map[uint64]string
	UseTestnet    bool
	MaxPeers      int
}

func DefaultMultiChainConfig(privateKey *ecdsa.PrivateKey) *MultiChainConfig {
	return &MultiChainConfig{
		PrivateKey:    privateKey,
		EnabledChains: []uint64{ChainIDEthereum, ChainIDBase, ChainIDOptimism},
		ListenPorts: map[uint64]string{

			ChainIDEthereum: ":30303",

		},
		RPCEndpoints: map[uint64]string{
			ChainIDEthereum: "ws://localhost:8546",
			ChainIDBase:     "ws://localhost:8646",
			ChainIDOptimism: "ws://localhost:8746",
		},
		UseTestnet: false,
		MaxPeers:   50,
	}
}

func TestnetMultiChainConfig(privateKey *ecdsa.PrivateKey) *MultiChainConfig {
	return &MultiChainConfig{
		PrivateKey:    privateKey,
		EnabledChains: []uint64{ChainIDSepolia, ChainIDBaseSep, ChainIDOptimSep},
		ListenPorts: map[uint64]string{

			ChainIDSepolia: ":30303",

		},
		UseTestnet: true,
		MaxPeers:   50,
	}
}

func NewMultiChainTransport(config *MultiChainConfig) (*MultiChainTransport, error) {
	if config.PrivateKey == nil {
		return nil, fmt.Errorf("private key required")
	}

	mct := &MultiChainTransport{
		chains:          make(map[uint64]*ChainTransport),
		unifiedFlow:     NewMempoolFlow(10000),
		config:          config,
		stopCh:          make(chan struct{}),
		priceAggregator: NewPriceAggregator(nil),
	}

	hasL2 := false
	for _, chainID := range config.EnabledChains {
		if IsOPStackL2(chainID) {
			hasL2 = true
			break
		}
	}

	fmt.Println("[MultiChain] Creating SHARED LibP2P transport (ONE instance for all chains)")
	sharedLibP2P, err := NewLibP2PTransport(config.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create shared libp2p: %w", err)
	}
	mct.sharedLibP2P = sharedLibP2P

	if err := sharedLibP2P.Start(); err != nil {
		sharedLibP2P.Stop()
		return nil, fmt.Errorf("failed to start shared libp2p: %w", err)
	}
	fmt.Println("[MultiChain] Shared LibP2P started - DHT + relay discovery running")

	for _, chainID := range config.EnabledChains {
		if IsOPStackL2(chainID) {
			continue
		}
		ct, err := mct.initChainTransport(chainID)
		if err != nil {
			for _, existing := range mct.chains {
				if existing.Hybrid != nil {
					existing.Hybrid.Stop()
				}
			}
			sharedLibP2P.Stop()
			return nil, fmt.Errorf("failed to init L1 chain %d: %w", chainID, err)
		}
		mct.chains[chainID] = ct
	}

	if hasL2 {
		opGossip, err := NewOpNodeGossipWithSharedHost(sharedLibP2P.Host(), config.PrivateKey)
		if err != nil {
			for _, existing := range mct.chains {
				if existing.Hybrid != nil {
					existing.Hybrid.Stop()
				}
			}
			sharedLibP2P.Stop()
			return nil, fmt.Errorf("failed to create OpNode gossip: %w", err)
		}
		mct.opNodeGossip = opGossip
		fmt.Println("[MultiChain] L2 GossipSub using SHARED LibP2P host")
	}

	for _, chainID := range config.EnabledChains {
		if !IsOPStackL2(chainID) {
			continue
		}
		ct, err := mct.initChainTransport(chainID)
		if err != nil {
			for _, existing := range mct.chains {
				if existing.Hybrid != nil {
					existing.Hybrid.Stop()
				}
			}
			if mct.opNodeGossip != nil {
				mct.opNodeGossip.Stop()
			}
			sharedLibP2P.Stop()
			return nil, fmt.Errorf("failed to init L2 chain %d: %w", chainID, err)
		}
		mct.chains[chainID] = ct
	}

	return mct, nil
}

func (mct *MultiChainTransport) initChainTransport(chainID uint64) (*ChainTransport, error) {

	var chainName string
	var chainConfig *ChainConfig

	mainnetConfigs := DefaultChainConfigs()
	if cfg, ok := mainnetConfigs[chainID]; ok {
		chainName = cfg.Name
		chainConfig = cfg
	}

	if chainName == "" {
		testnetConfigs := TestnetChainConfigs()
		if cfg, ok := testnetConfigs[chainID]; ok {
			chainName = cfg.Name
			chainConfig = cfg
		}
	}

	if chainName == "" {
		chainName = fmt.Sprintf("Chain-%d", chainID)
	}

	isL2 := IsOPStackL2(chainID)

	ct := &ChainTransport{
		ChainID:    chainID,
		Name:       chainName,
		Config:     chainConfig,
		IsL2:       isL2,
		trackedTxs: make(map[common.Hash]time.Time),
	}

	if isL2 {

		fmt.Printf("[MultiChain] Chain %d (%s) is L2 - using GossipSub for block gossip\n", chainID, chainName)
		return ct, nil
	}

	listenAddr := ":30303"
	if port, ok := mct.config.ListenPorts[chainID]; ok {
		listenAddr = port
	}

	hybrid, err := CreateHybridTransportForChain(chainID, mct.config.PrivateKey, listenAddr, mct.sharedLibP2P)
	if err != nil {
		return nil, fmt.Errorf("failed to create HybridTransport: %w", err)
	}
	ct.Hybrid = hybrid

	return ct, nil
}

func (mct *MultiChainTransport) Start() error {
	mct.mu.Lock()
	defer mct.mu.Unlock()

	if mct.running {
		return fmt.Errorf("already running")
	}

	if mct.opNodeGossip != nil {
		fmt.Println("[MultiChain] Starting OpNode gossip for L2 chains...")
		if err := mct.opNodeGossip.Start(); err != nil {
			return fmt.Errorf("failed to start OpNode gossip: %w", err)
		}

		mct.opNodeGossip.OnBlockReceived = mct.createL2BlockHandler()

		if err := mct.opNodeGossip.SubscribePriceGossip(); err != nil {
			fmt.Printf("[MultiChain] Warning: failed to subscribe to price gossip: %v\n", err)
		} else {

			mct.opNodeGossip.OnPriceGossip = mct.createPriceGossipHandler()
		}

		for chainID, ct := range mct.chains {
			if ct.IsL2 {
				if err := mct.opNodeGossip.SubscribeChain(chainID); err != nil {
					fmt.Printf("[MultiChain] Warning: failed to subscribe to L2 chain %d: %v\n", chainID, err)
				} else {
					fmt.Printf("[MultiChain] Chain %d (%s) L2 GossipSub subscribed\n", chainID, ct.Name)
				}
			}
		}
	}

	for chainID, ct := range mct.chains {

		if ct.IsL2 {
			continue
		}

		ct.Hybrid.OnTxHashReceived(mct.createTxHashHandler(chainID, ct))

		ct.Hybrid.OnTxReceived(mct.createTxReceivedHandler(chainID, ct))

		ct.Hybrid.OnZEAMMessage(mct.createZEAMMessageHandler(chainID))

		ct.Hybrid.OnPeerConnect(mct.createPeerConnectHandler(chainID))
		ct.Hybrid.OnPeerDrop(mct.createPeerDropHandler(chainID))

		ct.Hybrid.OnForeignNetworkPeer(mct.createForeignNetworkHandler(chainID))

		ct.Hybrid.OnPriceGossip(mct.createPriceGossipHandler())

		if err := ct.Hybrid.Start(); err != nil {

			fmt.Printf("[MultiChain] Warning: failed to start chain %d (%s): %v - continuing with other chains\n", chainID, ct.Name, err)
			continue
		}
		fmt.Printf("[MultiChain] Chain %d (%s) L1 DevP2P started\n", chainID, ct.Name)
	}

	mct.running = true

	mct.wg.Add(1)
	go mct.monitorLoop()

	return nil
}

func (mct *MultiChainTransport) createL2BlockHandler() func(chainID uint64, block *L2BlockGossip) {
	return func(chainID uint64, block *L2BlockGossip) {

		atomic.AddUint64(&mct.totalL2Blocks, 1)

		if ct, ok := mct.chains[chainID]; ok {
			atomic.AddUint64(&ct.blockCount, 1)
			ct.mu.Lock()
			ct.CurrentBlock = block.BlockNumber
			ct.LastBlockTime = block.ReceivedAt
			ct.mu.Unlock()
		}

		txHashes := block.ExtractTransactionHashes()
		if len(txHashes) > 0 {
			mct.unifiedFlow.Ingest(txHashes)
			atomic.AddUint64(&mct.totalTxSeen, uint64(len(txHashes)))

			if mct.OnTxReceived != nil {
				mct.OnTxReceived(chainID, txHashes)
			}
		}

		if mct.OnTxParsed != nil {
			fullTxs := block.ExtractFullTransactions()
			if len(fullTxs) > 0 {

				blockCount := atomic.LoadUint64(&mct.totalL2Blocks)
				if blockCount%10 == 1 {
					fmt.Printf("[L2] Block %d chain %d: %d/%d txs decoded\n",
						block.BlockNumber, chainID, len(fullTxs), len(block.Transactions))
				}
			}
			for _, tx := range fullTxs {
				mct.OnTxParsed(chainID, tx)
			}
		}

		if mct.OnL2Block != nil {
			mct.OnL2Block(chainID, block)
		}

		mct.emitFeed(NetworkEvent{
			Type:      EventL2Block,
			ChainID:   chainID,
			Timestamp: time.Now(),
			L2Block:   block,
		})

		if mct.OnBlockAdvance != nil {
			mct.OnBlockAdvance(chainID, block.BlockNumber)
		}
	}
}

func (mct *MultiChainTransport) createTxHashHandler(chainID uint64, ct *ChainTransport) func([]common.Hash) {
	return func(hashes []common.Hash) {

		mct.unifiedFlow.Ingest(hashes)

		atomic.AddUint64(&mct.totalTxSeen, uint64(len(hashes)))
		atomic.AddUint64(&ct.txCount, uint64(len(hashes)))

		ct.mu.Lock()
		now := time.Now()
		for _, h := range hashes {
			ct.trackedTxs[h] = now
		}

		cutoff := now.Add(-5 * time.Minute)
		for h, t := range ct.trackedTxs {
			if t.Before(cutoff) {
				delete(ct.trackedTxs, h)
			}
		}
		ct.mu.Unlock()

		if mct.OnTxReceived != nil {
			mct.OnTxReceived(chainID, hashes)
		}

		mct.emitFeed(NetworkEvent{
			Type:      EventTxHashes,
			ChainID:   chainID,
			Timestamp: time.Now(),
			Hashes:    hashes,
		})
	}
}

func (mct *MultiChainTransport) createTxReceivedHandler(chainID uint64, ct *ChainTransport) func([]*types.Transaction) {
	return func(txs []*types.Transaction) {

		atomic.AddUint64(&mct.totalTxSeen, uint64(len(txs)))
		atomic.AddUint64(&ct.txCount, uint64(len(txs)))

		if mct.OnTxParsed != nil {
			for _, tx := range txs {
				mct.OnTxParsed(chainID, tx)
			}
		}

		mct.emitFeed(NetworkEvent{
			Type:      EventTxFull,
			ChainID:   chainID,
			Timestamp: time.Now(),
			Txs:       txs,
		})
	}
}

func (mct *MultiChainTransport) createZEAMMessageHandler(chainID uint64) func(*types.Transaction, []byte) {
	return func(tx *types.Transaction, data []byte) {

	}
}

func (mct *MultiChainTransport) createPeerConnectHandler(chainID uint64) func(string) {
	return func(peerID string) {
		atomic.AddUint64(&mct.totalPeerSeen, 1)

		eventHash := crypto.Keccak256Hash([]byte(fmt.Sprintf("connect:%d:%s:%d", chainID, peerID, time.Now().UnixNano())))
		mct.unifiedFlow.Ingest([]common.Hash{eventHash})

		if ct, ok := mct.chains[chainID]; ok {
			atomic.AddInt32(&ct.PeerCount, 1)
		}

		if mct.OnPeerEvent != nil {
			mct.OnPeerEvent(chainID, peerID, true)
		}

		mct.emitFeed(NetworkEvent{
			Type:      EventPeerConnect,
			ChainID:   chainID,
			Timestamp: time.Now(),
			PeerID:    peerID,
		})
	}
}

func (mct *MultiChainTransport) createPeerDropHandler(chainID uint64) func(string) {
	return func(peerID string) {

		eventHash := crypto.Keccak256Hash([]byte(fmt.Sprintf("drop:%d:%s:%d", chainID, peerID, time.Now().UnixNano())))
		mct.unifiedFlow.Ingest([]common.Hash{eventHash})

		if ct, ok := mct.chains[chainID]; ok {
			atomic.AddInt32(&ct.PeerCount, -1)
		}

		if mct.OnPeerEvent != nil {
			mct.OnPeerEvent(chainID, peerID, false)
		}

		mct.emitFeed(NetworkEvent{
			Type:      EventPeerDrop,
			ChainID:   chainID,
			Timestamp: time.Now(),
			PeerID:    peerID,
		})
	}
}

func (mct *MultiChainTransport) createPriceGossipHandler() func([]byte) {
	return func(data []byte) {

		update, err := DecodePriceUpdate(data)
		if err != nil {

			return
		}

		if mct.priceAggregator != nil {
			if err := mct.priceAggregator.AddUpdate(update); err != nil {

				return
			}
		}

		if mct.OnPriceGossip != nil {
			mct.OnPriceGossip(update)
		}
	}
}

func (mct *MultiChainTransport) createForeignNetworkHandler(ourChainID uint64) func(string, uint64) {
	return func(peerID string, foreignNetworkID uint64) {

		eventHash := crypto.Keccak256Hash([]byte(fmt.Sprintf("foreign:%d:%d:%s:%d",
			ourChainID, foreignNetworkID, peerID, time.Now().UnixNano())))
		mct.unifiedFlow.Ingest([]common.Hash{eventHash})

		atomic.AddUint64(&mct.totalPeerSeen, 1)

		fmt.Printf("[MultiChain] PROMISCUOUS: Got peer from network %d via chain %d transport\n",
			foreignNetworkID, ourChainID)
	}
}

func (mct *MultiChainTransport) monitorLoop() {
	defer mct.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-mct.stopCh:
			return
		case <-ticker.C:
			mct.updateChainStates()
		}
	}
}

func (mct *MultiChainTransport) updateChainStates() {
	mct.mu.RLock()
	defer mct.mu.RUnlock()

	var chainStats []string

	for chainID, ct := range mct.chains {
		if ct.Hybrid != nil {
			peerCount := ct.Hybrid.PeerCount()
			atomic.StoreInt32(&ct.PeerCount, int32(peerCount))
			txCount := atomic.LoadUint64(&ct.txCount)
			chainStats = append(chainStats, fmt.Sprintf("L1-%d:%dp/%dt", chainID, peerCount, txCount))
		}
	}

	if mct.opNodeGossip != nil {
		l2Stats := mct.opNodeGossip.GetStats()
		for chainID, stats := range l2Stats {
			peers := 0
			blocks := uint64(0)
			if p, ok := stats["peer_count"].(int); ok {
				peers = p
			}
			if b, ok := stats["blocks_received"].(uint64); ok {
				blocks = b
			}
			chainStats = append(chainStats, fmt.Sprintf("L2-%d:%dp/%db", chainID, peers, blocks))
		}
	}

	if len(chainStats) > 0 && atomic.AddUint64(&mct.statsLogCount, 1)%6 == 0 {
		fmt.Printf("[Chains] %s\n", strings.Join(chainStats, " "))
	}
}

func (mct *MultiChainTransport) Stop() {
	mct.mu.Lock()
	defer mct.mu.Unlock()

	if !mct.running {
		return
	}

	close(mct.stopCh)

	for _, ct := range mct.chains {
		if ct.Hybrid != nil {
			ct.Hybrid.Stop()
		}
	}

	if mct.opNodeGossip != nil {
		mct.opNodeGossip.Stop()
	}

	if mct.sharedLibP2P != nil {
		mct.sharedLibP2P.Stop()
	}

	mct.wg.Wait()
	mct.running = false
}

func (mct *MultiChainTransport) BroadcastOnChain(chainID uint64, data []byte) (common.Hash, error) {
	mct.mu.RLock()
	ct, ok := mct.chains[chainID]
	mct.mu.RUnlock()

	if !ok {
		return common.Hash{}, fmt.Errorf("chain %d not enabled", chainID)
	}

	if ct.Hybrid == nil {
		return common.Hash{}, fmt.Errorf("chain %d is L2 (no HybridTransport) - use PublishL2 instead", chainID)
	}

	return ct.Hybrid.BroadcastZEAM(data)
}

func (mct *MultiChainTransport) GetL1Chains() []uint64 {
	mct.mu.RLock()
	defer mct.mu.RUnlock()

	chains := make([]uint64, 0)
	for chainID, ct := range mct.chains {
		if ct.Hybrid != nil && !ct.IsL2 {
			chains = append(chains, chainID)
		}
	}
	return chains
}

func (mct *MultiChainTransport) BroadcastRawTxOnChain(chainID uint64, signedTxBytes []byte) (common.Hash, error) {
	mct.mu.RLock()
	ct, ok := mct.chains[chainID]
	mct.mu.RUnlock()

	if !ok {
		return common.Hash{}, fmt.Errorf("chain %d not enabled", chainID)
	}

	var tx types.Transaction
	if err := tx.UnmarshalBinary(signedTxBytes); err != nil {
		return common.Hash{}, fmt.Errorf("failed to decode transaction: %w", err)
	}

	if err := ct.Hybrid.Broadcast(&tx); err != nil {
		return common.Hash{}, err
	}

	return tx.Hash(), nil
}

func (mct *MultiChainTransport) PublishL2(chainID uint64, data []byte) error {
	if mct.opNodeGossip == nil {
		return fmt.Errorf("L2 gossip not initialized")
	}
	if !IsOPStackL2(chainID) {
		return fmt.Errorf("chain %d is not an L2", chainID)
	}
	return mct.opNodeGossip.Publish(chainID, data)
}

func (mct *MultiChainTransport) PublishAllL2s(data []byte) map[uint64]error {
	if mct.opNodeGossip == nil {
		return map[uint64]error{0: fmt.Errorf("L2 gossip not initialized")}
	}
	return mct.opNodeGossip.PublishToAll(data)
}

func (mct *MultiChainTransport) BroadcastOnAllChains(data []byte) map[uint64]common.Hash {
	results := make(map[uint64]common.Hash)

	mct.mu.RLock()
	chains := make(map[uint64]*ChainTransport)
	for k, v := range mct.chains {
		chains[k] = v
	}
	mct.mu.RUnlock()

	var wg sync.WaitGroup
	var mu sync.Mutex

	for chainID, ct := range chains {
		wg.Add(1)
		go func(cid uint64, transport *ChainTransport) {
			defer wg.Done()

			if transport.IsL2 || transport.Hybrid == nil {

				if mct.opNodeGossip != nil {
					mct.opNodeGossip.Publish(cid, data)
				}
				return
			}

			hash, err := transport.Hybrid.BroadcastZEAM(data)
			if err == nil {
				mu.Lock()
				results[cid] = hash
				mu.Unlock()
			}
		}(chainID, ct)
	}

	wg.Wait()
	return results
}

func (mct *MultiChainTransport) GetUnifiedEntropy() []byte {
	return mct.unifiedFlow.Entropy()
}

func (mct *MultiChainTransport) HarvestEntropy() ([]byte, uint64) {
	return mct.unifiedFlow.Harvest()
}

func (mct *MultiChainTransport) GetFlowRate() float64 {
	_, flowRate, _ := mct.unifiedFlow.Stats()
	return flowRate
}

func (mct *MultiChainTransport) GetRecentHashes(count int) []common.Hash {
	return mct.unifiedFlow.RecentHashes(count)
}

func (mct *MultiChainTransport) GetFlowCompute() *FlowCompute {
	return NewFlowCompute(mct.unifiedFlow)
}

type ChainStatus struct {
	ChainID      uint64
	Name         string
	PeerCount    int
	TxCount      uint64
	SwapCount    uint64
	CurrentBlock uint64
	Connected    bool
}

func (mct *MultiChainTransport) GetChainStatus(chainID uint64) *ChainStatus {
	mct.mu.RLock()
	ct, ok := mct.chains[chainID]
	mct.mu.RUnlock()

	if !ok {
		return nil
	}

	var peerCount int
	var connected bool

	if ct.IsL2 {

		if mct.opNodeGossip != nil {
			peerCount = mct.opNodeGossip.GetPeerCount(chainID)
			connected = peerCount > 0
		}
	} else {

		peerCount = int(atomic.LoadInt32(&ct.PeerCount))
		connected = ct.Hybrid != nil && peerCount > 0
	}

	return &ChainStatus{
		ChainID:      chainID,
		Name:         ct.Name,
		PeerCount:    peerCount,
		TxCount:      atomic.LoadUint64(&ct.txCount),
		SwapCount:    atomic.LoadUint64(&ct.swapCount),
		CurrentBlock: ct.CurrentBlock,
		Connected:    connected,
	}
}

func (mct *MultiChainTransport) GetAllChainStatus() []*ChainStatus {
	mct.mu.RLock()
	defer mct.mu.RUnlock()

	statuses := make([]*ChainStatus, 0, len(mct.chains))
	for chainID := range mct.chains {
		if status := mct.GetChainStatus(chainID); status != nil {
			statuses = append(statuses, status)
		}
	}
	return statuses
}

func (mct *MultiChainTransport) GetTotalStats() (totalTx uint64, totalPeers int, flowRate float64) {
	totalTx = atomic.LoadUint64(&mct.totalTxSeen)

	mct.mu.RLock()
	for _, ct := range mct.chains {
		totalPeers += int(atomic.LoadInt32(&ct.PeerCount))
	}
	mct.mu.RUnlock()

	_, flowRate, _ = mct.unifiedFlow.Stats()
	return
}

func (mct *MultiChainTransport) IsRunning() bool {
	mct.mu.RLock()
	defer mct.mu.RUnlock()
	return mct.running
}

func (mct *MultiChainTransport) GetEnabledChains() []uint64 {
	mct.mu.RLock()
	defer mct.mu.RUnlock()

	chains := make([]uint64, 0, len(mct.chains))
	for chainID := range mct.chains {
		chains = append(chains, chainID)
	}
	return chains
}

func (mct *MultiChainTransport) Feed() NetworkFeed {
	mct.mu.Lock()
	defer mct.mu.Unlock()
	if mct.feed == nil {
		f := &multiChainFeed{mct: mct}
		f.fanOut.Init()
		mct.feed = f
	}
	return mct.feed
}

type multiChainFeed struct {
	mct    *MultiChainTransport
	fanOut EventFanOut
}

func (f *multiChainFeed) Subscribe(bufSize int) *FeedSubscription {
	return f.fanOut.Subscribe(bufSize)
}

func (f *multiChainFeed) Broadcast(payload []byte) <-chan BroadcastResult {

	f.mct.mu.RLock()
	var primary *HybridTransport
	for _, ct := range f.mct.chains {
		if ct.Hybrid != nil && !ct.IsL2 {
			primary = ct.Hybrid
			if ct.ChainID == 1 {
				break
			}
		}
	}
	f.mct.mu.RUnlock()

	ch := make(chan BroadcastResult, 1)
	if primary == nil {
		ch <- BroadcastResult{Err: fmt.Errorf("no L1 chain available")}
		close(ch)
		return ch
	}
	go func() {
		hash, err := primary.BroadcastZEAM(payload)
		ch <- BroadcastResult{TxHash: hash, Err: err}
		close(ch)
	}()
	return ch
}

func (f *multiChainFeed) BroadcastTx(tx *types.Transaction) <-chan BroadcastResult {
	f.mct.mu.RLock()
	var primary *HybridTransport
	for _, ct := range f.mct.chains {
		if ct.Hybrid != nil && !ct.IsL2 {
			primary = ct.Hybrid
			if ct.ChainID == 1 {
				break
			}
		}
	}
	f.mct.mu.RUnlock()

	ch := make(chan BroadcastResult, 1)
	if primary == nil {
		ch <- BroadcastResult{Err: fmt.Errorf("no L1 chain available")}
		close(ch)
		return ch
	}
	go func() {
		err := primary.Broadcast(tx)
		var hash common.Hash
		if tx != nil {
			hash = tx.Hash()
		}
		ch <- BroadcastResult{TxHash: hash, Err: err}
		close(ch)
	}()
	return ch
}

func (f *multiChainFeed) Stats() NetworkStats {
	f.mct.mu.RLock()
	defer f.mct.mu.RUnlock()

	stats := NetworkStats{
		L2Peers: make(map[uint64]int),
	}
	for chainID, ct := range f.mct.chains {
		peers := int(atomic.LoadInt32(&ct.PeerCount))
		if ct.IsL2 {
			stats.L2Peers[chainID] = peers
		} else {
			stats.L1Peers += peers
		}
	}
	stats.MempoolRate = f.mct.GetFlowRate()
	return stats
}

func (f *multiChainFeed) Start() error { return nil }
func (f *multiChainFeed) Stop()        {}

func (mct *MultiChainTransport) emitFeed(event NetworkEvent) {
	if mct.feed != nil {
		mct.feed.fanOut.Emit(event)
	}
}

func (mct *MultiChainTransport) GetPriceAggregator() *PriceAggregator {
	return mct.priceAggregator
}

func (mct *MultiChainTransport) GetAggregatedPrice(chainID uint64, pool common.Address) *AggregatedPrice {
	if mct.priceAggregator == nil {
		return nil
	}
	return mct.priceAggregator.GetPrice(chainID, pool)
}

func (mct *MultiChainTransport) EnablePriceBroadcaster() {
	if mct.priceBroadcaster != nil {
		return
	}

	mct.priceBroadcaster = NewPriceBroadcaster(mct.config.PrivateKey, mct.broadcastPriceData)
	fmt.Println("[MultiChain] Price broadcaster enabled - this node will broadcast prices")
}

func (mct *MultiChainTransport) broadcastPriceData(chainID uint64, data []byte) error {

	if IsOPStackL2(chainID) && mct.opNodeGossip != nil {
		return mct.opNodeGossip.PublishPriceGossip(data)
	}

	mct.mu.RLock()
	ct, ok := mct.chains[chainID]
	mct.mu.RUnlock()

	if !ok || ct.Hybrid == nil {

		for _, chain := range mct.chains {
			if chain.Hybrid != nil && !chain.IsL2 {
				chain.Hybrid.BroadcastPriceGossip(data)
			}
		}

		if mct.opNodeGossip != nil {
			mct.opNodeGossip.PublishPriceGossip(data)
		}
		return nil
	}

	_, err := ct.Hybrid.BroadcastPriceGossip(data)
	return err
}

func (mct *MultiChainTransport) BroadcastPrice(chainID uint64, pool common.Address, r0, r1 *big.Int, blockNum uint64) error {
	if mct.priceBroadcaster == nil {
		return fmt.Errorf("price broadcaster not enabled - call EnablePriceBroadcaster first")
	}
	return mct.priceBroadcaster.BroadcastPrice(chainID, pool, r0, r1, blockNum)
}

func (mct *MultiChainTransport) GetPriceGossipStats() map[string]interface{} {
	stats := make(map[string]interface{})

	if mct.priceAggregator != nil {
		stats["aggregator"] = mct.priceAggregator.Stats()
	}

	if mct.priceBroadcaster != nil {
		stats["broadcaster"] = mct.priceBroadcaster.Stats()
	}

	if mct.opNodeGossip != nil {
		stats["gossipsub_peers"] = mct.opNodeGossip.GetPriceGossipPeerCount()
	}

	return stats
}

func (mct *MultiChainTransport) GetLibP2PInfo() map[string]interface{} {
	mct.mu.RLock()
	defer mct.mu.RUnlock()

	for _, ct := range mct.chains {
		if ct.Hybrid != nil && !ct.IsL2 {
			return map[string]interface{}{
				"enabled":         true,
				"peer_id":         ct.Hybrid.LibP2PPeerID(),
				"relay_addresses": ct.Hybrid.RelayAddrs(),
			}
		}
	}

	return map[string]interface{}{"enabled": false}
}

func (mct *MultiChainTransport) SharedHost() host.Host {
	if mct.sharedLibP2P != nil {
		return mct.sharedLibP2P.Host()
	}
	return nil
}

func (mct *MultiChainTransport) JoinContentTopic(name string) (*pubsub.Topic, *pubsub.Subscription, error) {
	if mct.opNodeGossip == nil {
		return nil, nil, fmt.Errorf("gossip not initialized")
	}
	return mct.opNodeGossip.JoinCustomTopic(name)
}
