package node

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/libp2p/go-libp2p/core/host"
)

type LibP2PHostProvider interface {
	LibP2PHost() host.Host
}

type HybridTransport struct {
	libp2p     *LibP2PTransport
	ownsLibP2P bool
	devp2p     *DevP2PNode
	p2p        *P2PTransport

	privateKey *ecdsa.PrivateKey
	config     *DevP2PConfig

	relayConnections  int
	directConnections int
	statsMu           sync.RWMutex

	running bool
	stopCh  chan struct{}
}

type HybridConfig struct {
	PrivateKey     *ecdsa.PrivateKey
	DevP2PConfig   *DevP2PConfig
	SharedLibP2P   *LibP2PTransport
}

func NewHybridTransport(cfg *HybridConfig) (*HybridTransport, error) {
	h := &HybridTransport{
		privateKey:    cfg.PrivateKey,
		config:        cfg.DevP2PConfig,
		stopCh:        make(chan struct{}),
		ownsLibP2P:    cfg.SharedLibP2P == nil,
	}

	if cfg.SharedLibP2P != nil {
		h.libp2p = cfg.SharedLibP2P
	} else {
		libp2pTransport, err := NewLibP2PTransport(cfg.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create libp2p transport: %w", err)
		}
		h.libp2p = libp2pTransport
	}

	devp2pNode, err := NewDevP2PNode(cfg.DevP2PConfig)
	if err != nil {
		if h.ownsLibP2P {
			h.libp2p.Stop()
		}
		return nil, fmt.Errorf("failed to create devp2p node: %w", err)
	}
	h.devp2p = devp2pNode

	return h, nil
}

func (h *HybridTransport) Start() error {

	if h.ownsLibP2P {
		if err := h.libp2p.Start(); err != nil {
			return fmt.Errorf("failed to start libp2p: %w", err)
		}
	}

	go h.waitForRelays()

	if err := h.devp2p.Start(); err != nil {
		if h.ownsLibP2P {
			h.libp2p.Stop()
		}
		return fmt.Errorf("failed to start devp2p: %w", err)
	}

	h.running = true

	go h.monitorConnections()

	return nil
}

func (h *HybridTransport) waitForRelays() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	logged := false
	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			addrs := h.libp2p.GetRelayAddrs()
			if len(addrs) > 0 {
				h.statsMu.Lock()
				h.relayConnections = len(addrs)
				h.statsMu.Unlock()

				if !logged {
					fmt.Printf("[Hybrid] Got %d relay addresses for NAT traversal\n", len(addrs))
					logged = true
				}
			}
		}
	}
}

func (h *HybridTransport) monitorConnections() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			devp2pPeers := h.devp2p.PeerCount()

			h.statsMu.Lock()
			h.directConnections = devp2pPeers
			h.statsMu.Unlock()

			if devp2pPeers > 0 {
				fmt.Printf("[Hybrid] L1 peers: %d\n", devp2pPeers)
			}
		}
	}
}

func (h *HybridTransport) Stop() {
	if !h.running {
		return
	}
	h.running = false
	close(h.stopCh)

	h.devp2p.Stop()

	if h.ownsLibP2P {
		h.libp2p.Stop()
	}

	fmt.Printf("[Hybrid] Stopped\n")
}

func (h *HybridTransport) Broadcast(tx *types.Transaction) error {
	return h.devp2p.BroadcastRawTx(tx)
}

func (h *HybridTransport) BroadcastZEAM(data []byte) (common.Hash, error) {
	return h.devp2p.BroadcastZEAM(data)
}

func (h *HybridTransport) PeerCount() int {
	return h.devp2p.PeerCount()
}

func (h *HybridTransport) Stats() (relays, direct int) {
	h.statsMu.RLock()
	defer h.statsMu.RUnlock()
	return h.relayConnections, h.directConnections
}

func (h *HybridTransport) LibP2PPeerID() string {
	return h.libp2p.PeerID()
}

func (h *HybridTransport) RelayAddrs() []string {
	return h.libp2p.GetRelayAddrs()
}

func (h *HybridTransport) LibP2PHost() host.Host {
	return h.libp2p.Host()
}

func (h *HybridTransport) Address() common.Address {
	return h.devp2p.address
}

func (h *HybridTransport) OnZEAMMessage(fn func(tx *types.Transaction, data []byte)) {
	h.devp2p.OnZEAMMessage = fn
}

func (h *HybridTransport) OnPeerConnect(fn func(peerID string)) {
	h.devp2p.OnPeerConnect = fn
}

func (h *HybridTransport) OnPeerDrop(fn func(peerID string)) {
	h.devp2p.OnPeerDrop = fn
}

func (h *HybridTransport) OnTxHashReceived(fn func(hashes []common.Hash)) {
	h.devp2p.OnTxHashReceived = fn
}

func (h *HybridTransport) OnTxReceived(fn func(txs []*types.Transaction)) {
	h.devp2p.OnTxReceived = fn
}

func (h *HybridTransport) OnBlockHashReceived(fn func(hash common.Hash, number uint64)) {
	h.devp2p.OnBlockHashReceived = fn
}

func (h *HybridTransport) OnForeignNetworkPeer(fn func(peerID string, networkID uint64)) {
	h.devp2p.OnForeignNetworkPeer = fn
}

func (h *HybridTransport) OnPriceGossip(fn func(data []byte)) {
	h.devp2p.OnPriceGossip = fn
}

func (h *HybridTransport) BroadcastPriceGossip(data []byte) (common.Hash, error) {
	return h.devp2p.BroadcastPriceGossip(data)
}

func CreateSepoliaHybridTransport(privateKey *ecdsa.PrivateKey, listenAddr string, sharedLibP2P *LibP2PTransport) (*HybridTransport, error) {
	devp2pConfig := SepoliaConfig(privateKey)
	devp2pConfig.ListenAddr = listenAddr
	devp2pConfig.Bootnodes = SepoliaBootnodes()

	return NewHybridTransport(&HybridConfig{
		PrivateKey:   privateKey,
		DevP2PConfig: devp2pConfig,
		SharedLibP2P: sharedLibP2P,
	})
}

func CreateHybridTransportForChain(chainID uint64, privateKey *ecdsa.PrivateKey, listenAddr string, sharedLibP2P *LibP2PTransport) (*HybridTransport, error) {
	var devp2pConfig *DevP2PConfig

	switch chainID {
	case 1:
		devp2pConfig = MainnetConfig(privateKey)
		devp2pConfig.ListenAddr = listenAddr
		devp2pConfig.Bootnodes = MainnetBootnodes()

	case 11155111:
		devp2pConfig = SepoliaConfig(privateKey)
		devp2pConfig.ListenAddr = listenAddr
		devp2pConfig.Bootnodes = SepoliaBootnodes()

	default:
		return nil, fmt.Errorf("chain %d not supported for DevP2P (only mainnet=1 and Sepolia=11155111)", chainID)
	}

	return NewHybridTransport(&HybridConfig{
		PrivateKey:   privateKey,
		DevP2PConfig: devp2pConfig,
		SharedLibP2P: sharedLibP2P,
	})
}

func (h *HybridTransport) ChainID() *big.Int {
	return h.config.ChainID
}

func (h *HybridTransport) NetworkID() uint64 {
	return h.config.NetworkID
}
