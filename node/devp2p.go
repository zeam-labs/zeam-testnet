package node

import (
	"crypto/ecdsa"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"math/big"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

const (

	ETH68            = 68
	protocolName     = "eth"
	protocolMaxMsgSize = 10 * 1024 * 1024

	StatusMsg                    = 0x00
	NewBlockHashesMsg            = 0x01
	TransactionsMsg              = 0x02
	GetBlockHeadersMsg           = 0x03
	BlockHeadersMsg              = 0x04
	GetBlockBodiesMsg            = 0x05
	BlockBodiesMsg               = 0x06
	NewBlockMsg                  = 0x07
	NewPooledTransactionHashesMsg = 0x08
	GetPooledTransactionsMsg     = 0x09
	PooledTransactionsMsg        = 0x0a

	ZEAMMagic0 = 0x5A
	ZEAMMagic1 = 0x45

	ZEAMMsgTypeCoordination = 0x01
	ZEAMMsgTypePriceGossip  = 0x02
)

type DevP2PNode struct {
	server     *p2p.Server
	privateKey *ecdsa.PrivateKey
	address    common.Address

	networkID   uint64
	chainID     *big.Int
	genesisHash common.Hash
	forkID      forkid.ID
	td          *big.Int

	currentHead common.Hash
	headMu      sync.RWMutex

	nonceOffset uint64

	peers   map[string]*ethPeer
	peersMu sync.RWMutex

	txPool     map[common.Hash]*types.Transaction
	txPoolMu   sync.RWMutex
	seenTxs    map[common.Hash]bool
	seenTxsMu  sync.RWMutex

	OnZEAMMessage       func(tx *types.Transaction, data []byte)
	OnTxHashReceived    func(hashes []common.Hash)
	OnTxReceived        func(txs []*types.Transaction)
	OnPeerConnect       func(peerID string)
	OnPeerDrop          func(peerID string)
	OnForeignNetworkPeer func(peerID string, networkID uint64)
	OnBlockHashReceived  func(hash common.Hash, number uint64)
	OnPriceGossip       func(data []byte)

	fullTxReceived   uint64
	hashesRequested  uint64

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

type ethPeer struct {
	id        string
	peer      *p2p.Peer
	rw        p2p.MsgReadWriter
	version   uint
	networkID uint64
	genesis   common.Hash
	knownTxs  map[common.Hash]bool
	mu        sync.RWMutex
}

type StatusPacket struct {
	ProtocolVersion uint32
	NetworkID       uint64
	TD              *big.Int
	Head            common.Hash
	Genesis         common.Hash
	ForkID          forkid.ID
}

type NewPooledTransactionHashesPacket68 struct {
	Types  []byte
	Sizes  []uint32
	Hashes []common.Hash
}

type GetPooledTransactionsPacket struct {
	RequestId uint64
	Hashes    []common.Hash
}

type PooledTransactionsPacket struct {
	RequestId    uint64
	Transactions []*types.Transaction
}

type DevP2PConfig struct {
	PrivateKey  *ecdsa.PrivateKey
	NetworkID   uint64
	ChainID     *big.Int
	GenesisHash common.Hash
	ForkID      forkid.ID
	TD          *big.Int
	ListenAddr  string
	MaxPeers    int
	Bootnodes   []*enode.Node
	NonceOffset uint64
}

func computeForkID(config *params.ChainConfig, genesisHash common.Hash, headTime uint64) forkid.ID {

	hash := crc32.ChecksumIEEE(genesisHash[:])

	headBlock := uint64(22_000_000)
	blockForks := gatherBlockForks(config)
	timeForks := gatherTimeForks(config)

	allForks := append(blockForks, timeForks...)
	sort.Slice(allForks, func(i, j int) bool { return allForks[i] < allForks[j] })

	var uniqueForks []uint64
	for i, f := range allForks {
		if i == 0 || f != allForks[i-1] {
			uniqueForks = append(uniqueForks, f)
		}
	}

	var nextFork uint64
	for _, fork := range uniqueForks {

		passed := false
		if fork < 1_000_000_000 {
			passed = fork <= headBlock
		} else {
			passed = fork <= headTime
		}

		if passed {
			hash = checksumUpdateFork(hash, fork)
		} else if nextFork == 0 {
			nextFork = fork
		}
	}

	return forkid.ID{
		Hash: [4]byte{byte(hash >> 24), byte(hash >> 16), byte(hash >> 8), byte(hash)},
		Next: nextFork,
	}
}

func checksumUpdateFork(hash uint32, fork uint64) uint32 {
	var blob [8]byte
	binary.BigEndian.PutUint64(blob[:], fork)
	return crc32.Update(hash, crc32.IEEETable, blob[:])
}

func gatherBlockForks(config *params.ChainConfig) []uint64 {
	var forks []uint64

	add := func(f *big.Int) {
		if f != nil && f.Uint64() > 0 {
			forks = append(forks, f.Uint64())
		}
	}
	add(config.HomesteadBlock)
	if config.DAOForkSupport {
		add(config.DAOForkBlock)
	}
	add(config.EIP150Block)
	add(config.EIP155Block)
	add(config.EIP158Block)
	add(config.ByzantiumBlock)
	add(config.ConstantinopleBlock)
	add(config.PetersburgBlock)
	add(config.IstanbulBlock)
	add(config.MuirGlacierBlock)
	add(config.BerlinBlock)
	add(config.LondonBlock)
	add(config.ArrowGlacierBlock)
	add(config.GrayGlacierBlock)
	add(config.MergeNetsplitBlock)
	return forks
}

func gatherTimeForks(config *params.ChainConfig) []uint64 {
	var forks []uint64
	if config.ShanghaiTime != nil {
		forks = append(forks, *config.ShanghaiTime)
	}
	if config.CancunTime != nil {
		forks = append(forks, *config.CancunTime)
	}
	if config.PragueTime != nil {
		forks = append(forks, *config.PragueTime)
	}

	if config.OsakaTime != nil {
		forks = append(forks, *config.OsakaTime)
	}
	if config.BPO1Time != nil {
		forks = append(forks, *config.BPO1Time)
	}
	if config.BPO2Time != nil {
		forks = append(forks, *config.BPO2Time)
	}
	return forks
}

func SepoliaConfig(privateKey *ecdsa.PrivateKey) *DevP2PConfig {
	td := new(big.Int)
	td.SetString("17000000000000000", 10)
	genesisHash := common.HexToHash("0x25a5cc106eea7138acab33231d7160d69cb777ee0c2c553fcddf5138993e6dd9")

	forkID := computeForkID(params.SepoliaChainConfig, genesisHash, uint64(time.Now().Unix()))
	return &DevP2PConfig{
		PrivateKey:  privateKey,
		NetworkID:   11155111,
		ChainID:     big.NewInt(11155111),
		GenesisHash: genesisHash,
		ForkID:      forkID,
		TD:          td,
		ListenAddr:  ":30303",
		MaxPeers:    25,
		NonceOffset: 100000,
	}
}

func SepoliaBootnodes() []*enode.Node {
	urls := []string{

		"enode://4e5e92199ee224a01932a377160aa432f31d0b351f84ab413a8e0a42f4f36476f8fb1cbe914af0d9aef0d51665c214cf653c651c4bbd9d5550a934f241f1682b@138.197.51.181:30303",
		"enode://143e11fb766781d22d92a2e33f8f104cddae4411a122295ed1fdb6638de96a6ce65f5b7c964ba3763bba27961738fef7d3ecc739268f3e5e771fb4c87b6234ba@146.190.1.103:30303",
		"enode://8b61dc2d06c3f96fddcbebb0efb29d60d3598650275dc469c22229d3e5620369b0d3dedafd929835fe7f489618f19f456fe7c0df572bf2d914a9f4e006f783a9@170.64.250.88:30303",
		"enode://10d62eff032205fcef19497f35ca8477bea0eadfff6d769a147e895d8b2b8f8ae6341630c645c30f5df6e67547c03494ced3d9c5764e8622a26587b083b028e8@139.59.49.206:30303",
		"enode://9e9492e2e8836114cc75f5b929784f4f46c324ad01daf87d956f98b3b6c5fcba95524d6e5cf9861dc96a2c8a171ea7105bb554a197455058de185fa870970c7c@138.68.123.152:30303",
	}

	nodes := make([]*enode.Node, 0, len(urls))
	for _, url := range urls {
		node, err := enode.Parse(enode.ValidSchemes, url)
		if err == nil {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func MainnetConfig(privateKey *ecdsa.PrivateKey) *DevP2PConfig {
	td := new(big.Int)
	td.SetString("58750000000000000000000", 10)
	genesisHash := common.HexToHash("0xd4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3")

	forkID := computeForkID(params.MainnetChainConfig, genesisHash, uint64(time.Now().Unix()))
	return &DevP2PConfig{
		PrivateKey:  privateKey,
		NetworkID:   1,
		ChainID:     big.NewInt(1),
		GenesisHash: genesisHash,
		ForkID:      forkID,
		TD:          td,
		ListenAddr:  ":30303",
		MaxPeers:    50,
		NonceOffset: 100000,
	}
}

func MainnetBootnodes() []*enode.Node {
	urls := []string{

		"enode://d860a01f9722d78051619d1e2351aba3f43f943f6f00718d1b9baa4101932a1f5011f16bb2b1bb35db20d6fe28fa0bf09636d26a87d31de9ec6203eeedb1f666@18.138.108.67:30303",
		"enode://22a8232c3abc76a16ae9d6c3b164f98775fe226f0917b0ca871128a74a8e9630b458460865bab457221f1d448dd9791d24c4e5d88786180ac185df813a68d4de@3.209.45.79:30303",
		"enode://2b252ab6a1d0f971d9722cb839a42cb81db019ba44c08754628ab4a823487071b5695317c8ccd085219c3a03af063495b2f1da8d18218da2d6a82981b45e6ffc@65.108.70.101:30303",
		"enode://a979fb575495b8d6db44f750317d0f4622bf4c2aa3365d6af7c284339968eef29b69ad0dce72a4d8db5ebb4968de0e3bec910127f134779fbcb0cb6d3331163c@52.16.188.185:30303",
		"enode://3f1d12044546b76342d59d4a05532c14b85aa669704bfe1f864fe079415aa2c02d743e03218e57a33fb94523adb54032871a6c51b2cc5514cb7c7e35b3ed0a99@13.93.211.84:30303",
		"enode://78de8a0916848093c73790ead81d1928bec737d565119932b98c6b100d944b7a95e94f847f689fc723399d2e31129d182f7ef3863f2b4c820abbf3ab2722344d@191.235.84.50:30303",
	}

	nodes := make([]*enode.Node, 0, len(urls))
	for _, url := range urls {
		node, err := enode.Parse(enode.ValidSchemes, url)
		if err == nil {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func NewDevP2PNode(cfg *DevP2PConfig) (*DevP2PNode, error) {
	fmt.Println("[DevP2P] Creating node...")
	if cfg.PrivateKey == nil {
		return nil, errors.New("private key required")
	}

	addr := crypto.PubkeyToAddress(cfg.PrivateKey.PublicKey)
	fmt.Printf("[DevP2P] Address: %s\n", addr.Hex()[:10])

	node := &DevP2PNode{
		privateKey:  cfg.PrivateKey,
		address:     addr,
		networkID:   cfg.NetworkID,
		chainID:     cfg.ChainID,
		genesisHash: cfg.GenesisHash,
		forkID:      cfg.ForkID,
		td:          cfg.TD,
		nonceOffset: cfg.NonceOffset,
		peers:       make(map[string]*ethPeer),
		txPool:      make(map[common.Hash]*types.Transaction),
		seenTxs:     make(map[common.Hash]bool),
		stopCh:      make(chan struct{}),
	}

	fmt.Println("[DevP2P] Creating eth/68 protocol...")
	proto := p2p.Protocol{
		Name:    protocolName,
		Version: ETH68,
		Length:  17,
		Run:     node.runPeer,
		NodeInfo: func() interface{} {

			node.headMu.RLock()
			head := node.currentHead
			if head == (common.Hash{}) {
				head = node.genesisHash
			}
			node.headMu.RUnlock()
			return &ethNodeInfo{
				Network: cfg.NetworkID,
				Genesis: cfg.GenesisHash,
				Config:  nil,
				Head:    head,
			}
		},
		PeerInfo: func(id enode.ID) interface{} {
			return nil
		},
	}

	fmt.Printf("[DevP2P] Creating server on %s...\n", cfg.ListenAddr)
	serverCfg := p2p.Config{
		PrivateKey:      cfg.PrivateKey,
		Name:            "ZEAM/v1.0.0",
		MaxPeers:        cfg.MaxPeers,
		Protocols:       []p2p.Protocol{proto},
		ListenAddr:      cfg.ListenAddr,
		NAT:             nat.Any(),
		BootstrapNodes:  cfg.Bootnodes,
		StaticNodes:     cfg.Bootnodes,
		DiscoveryV4:     true,
		MaxPendingPeers: 10,
	}

	node.server = &p2p.Server{Config: serverCfg}
	fmt.Println("[DevP2P] Node created successfully")

	return node, nil
}

type ethNodeInfo struct {
	Network uint64      `json:"network"`
	Genesis common.Hash `json:"genesis"`
	Config  interface{} `json:"config"`
	Head    common.Hash `json:"head"`
}

func (n *DevP2PNode) Start() error {
	fmt.Println("[DevP2P] Starting server...")
	if n.running {
		return errors.New("already running")
	}

	fmt.Println("[DevP2P] Calling server.Start()...")
	if err := n.server.Start(); err != nil {
		fmt.Printf("[DevP2P] ERROR: server.Start() failed: %v\n", err)
		return fmt.Errorf("failed to start P2P server: %w", err)
	}

	n.running = true
	fmt.Printf("[DevP2P] Started on %s (address: %s)\n", n.server.ListenAddr, n.address.Hex())
	fmt.Printf("[DevP2P] Network: %d, Genesis: %s\n", n.networkID, n.genesisHash.Hex()[:18])
	fmt.Printf("[DevP2P] Enode: %s\n", n.server.Self().URLv4())

	return nil
}

func (n *DevP2PNode) Stop() {
	if !n.running {
		return
	}

	close(n.stopCh)
	n.wg.Wait()
	n.server.Stop()
	n.running = false

	fmt.Printf("[DevP2P] Stopped\n")
}

func (n *DevP2PNode) runPeer(peer *p2p.Peer, rw p2p.MsgReadWriter) error {
	peerID := peer.ID().String()[:16]
	fmt.Printf("[DevP2P] Peer connected: %s\n", peerID)

	ep := &ethPeer{
		id:       peerID,
		peer:     peer,
		rw:       rw,
		knownTxs: make(map[common.Hash]bool),
	}

	n.peersMu.Lock()
	n.peers[peerID] = ep
	n.peersMu.Unlock()

	if n.OnPeerConnect != nil {
		n.OnPeerConnect(peerID)
	}

	if err := n.doHandshake(ep); err != nil {
		fmt.Printf("[DevP2P] Handshake failed with %s: %v\n", peerID, err)
		n.removePeer(peerID)
		return err
	}

	fmt.Printf("[DevP2P] Handshake complete with %s (version=%d)\n", peerID, ep.version)

	n.wg.Add(1)
	defer n.wg.Done()
	defer n.removePeer(peerID)

	for {
		select {
		case <-n.stopCh:
			return nil
		default:
		}

		msg, err := rw.ReadMsg()
		if err != nil {
			fmt.Printf("[DevP2P] Read error from %s: %v\n", peerID, err)
			return err
		}

		if err := n.handleMsg(ep, msg); err != nil {
			fmt.Printf("[DevP2P] Handle error from %s: %v\n", peerID, err)
			msg.Discard()
			return err
		}
		msg.Discard()
	}
}

func (n *DevP2PNode) doHandshake(ep *ethPeer) error {

	head := n.genesisHash
	n.headMu.RLock()
	if n.currentHead != (common.Hash{}) {
		head = n.currentHead
	}
	n.headMu.RUnlock()

	status := &StatusPacket{
		ProtocolVersion: ETH68,
		NetworkID:       n.networkID,
		TD:              n.td,
		Head:            head,
		Genesis:         n.genesisHash,
		ForkID:          n.forkID,
	}

	if err := p2p.Send(ep.rw, StatusMsg, status); err != nil {
		return fmt.Errorf("failed to send status: %w", err)
	}

	msg, err := ep.rw.ReadMsg()
	if err != nil {
		return fmt.Errorf("failed to read status: %w", err)
	}
	if msg.Code != StatusMsg {
		return fmt.Errorf("expected status, got %d", msg.Code)
	}

	var peerStatus StatusPacket
	if err := msg.Decode(&peerStatus); err != nil {
		return fmt.Errorf("failed to decode status: %w", err)
	}

	ep.networkID = peerStatus.NetworkID
	ep.genesis = peerStatus.Genesis

	if peerStatus.NetworkID != n.networkID {
		fmt.Printf("[DevP2P] PROMISCUOUS: Accepting peer from network %d (we are %d) - harvesting entropy\n",
			peerStatus.NetworkID, n.networkID)
		if n.OnForeignNetworkPeer != nil {
			n.OnForeignNetworkPeer(ep.id, peerStatus.NetworkID)
		}
	}

	n.headMu.Lock()
	n.currentHead = peerStatus.Head
	n.headMu.Unlock()

	ep.version = uint(peerStatus.ProtocolVersion)
	return nil
}

func (n *DevP2PNode) handleMsg(ep *ethPeer, msg p2p.Msg) error {

	if msg.Code == NewPooledTransactionHashesMsg || msg.Code == TransactionsMsg {
		fmt.Printf("[DevP2P] Chain %d: msg type 0x%02x from %s\n", n.networkID, msg.Code, ep.id[:8])
	}

	switch msg.Code {
	case TransactionsMsg:

		var txs []*types.Transaction
		if err := msg.Decode(&txs); err != nil {

			fmt.Printf("[DevP2P] Ignoring tx decode error from %s: %v\n", ep.id, err)
			return nil
		}
		n.handleTransactions(ep, txs)

	case NewPooledTransactionHashesMsg:

		var packet NewPooledTransactionHashesPacket68
		if err := msg.Decode(&packet); err != nil {
			fmt.Printf("[DevP2P] Ignoring hash announcement decode error from %s: %v\n", ep.id, err)
			return nil
		}
		n.handleNewPooledTxHashes(ep, &packet)

	case GetPooledTransactionsMsg:

		var packet GetPooledTransactionsPacket
		if err := msg.Decode(&packet); err != nil {
			fmt.Printf("[DevP2P] Ignoring get-tx decode error from %s: %v\n", ep.id, err)
			return nil
		}
		n.handleGetPooledTransactions(ep, &packet)

	case PooledTransactionsMsg:

		payload := make([]byte, msg.Size)
		if _, err := msg.Payload.Read(payload); err != nil {
			return nil
		}

		var packet PooledTransactionsPacket
		if err := rlp.DecodeBytes(payload, &packet); err != nil {

			n.handlePooledTransactionsRawBytes(ep, payload)
			return nil
		}
		n.handlePooledTransactions(ep, &packet)

	case NewBlockHashesMsg:

		if n.OnBlockHashReceived != nil {
			var announces []struct {
				Hash   common.Hash
				Number uint64
			}
			if err := msg.Decode(&announces); err == nil {
				for _, a := range announces {
					n.OnBlockHashReceived(a.Hash, a.Number)
				}
			}
		}
		return nil

	case GetBlockHeadersMsg:

		var req struct {
			RequestId uint64
			Origin    interface{}
			Amount    uint64
			Skip      uint64
			Reverse   bool
		}
		if err := msg.Decode(&req); err != nil {
			return nil
		}

		resp := struct {
			RequestId uint64
			Headers   []*types.Header
		}{
			RequestId: req.RequestId,
			Headers:   []*types.Header{},
		}
		p2p.Send(ep.rw, BlockHeadersMsg, resp)
		return nil

	case GetBlockBodiesMsg:

		var req struct {
			RequestId uint64
			Hashes    []common.Hash
		}
		if err := msg.Decode(&req); err != nil {
			return nil
		}
		resp := struct {
			RequestId uint64
			Bodies    []struct{}
		}{
			RequestId: req.RequestId,
			Bodies:    []struct{}{},
		}
		p2p.Send(ep.rw, BlockBodiesMsg, resp)
		return nil

	case NewBlockMsg, BlockHeadersMsg, BlockBodiesMsg:

		return nil

	default:

		return nil
	}

	return nil
}

func (n *DevP2PNode) handleTransactions(ep *ethPeer, txs []*types.Transaction) {

	var newTxs []*types.Transaction

	for _, tx := range txs {
		hash := tx.Hash()

		n.seenTxsMu.Lock()
		if n.seenTxs[hash] {
			n.seenTxsMu.Unlock()
			continue
		}
		n.seenTxs[hash] = true
		n.seenTxsMu.Unlock()

		ep.mu.Lock()
		ep.knownTxs[hash] = true
		ep.mu.Unlock()

		newTxs = append(newTxs, tx)

		data := tx.Data()
		if len(data) >= 4 && data[0] == ZEAMMagic0 && data[1] == ZEAMMagic1 {
			msgType := data[3]
			switch msgType {
			case ZEAMMsgTypePriceGossip:

				if n.OnPriceGossip != nil {
					n.OnPriceGossip(data[4:])
				}
			default:

				fmt.Printf("[DevP2P] ZEAM TX: %s (version=0x%02x, type=0x%02x, %d bytes)\n",
					hash.Hex()[:18], data[2], msgType, len(data))
				if n.OnZEAMMessage != nil {
					n.OnZEAMMessage(tx, data)
				}
			}
		}
	}

	if len(newTxs) > 0 && n.OnTxReceived != nil {
		n.OnTxReceived(newTxs)
	}
}

func (n *DevP2PNode) handleNewPooledTxHashes(ep *ethPeer, packet *NewPooledTransactionHashesPacket68) {

	var wanted []common.Hash
	n.seenTxsMu.RLock()
	for _, hash := range packet.Hashes {
		if !n.seenTxs[hash] {
			wanted = append(wanted, hash)
		}
	}
	n.seenTxsMu.RUnlock()

	if len(wanted) == 0 {
		return
	}

	n.seenTxsMu.Lock()
	for _, hash := range wanted {
		n.seenTxs[hash] = true
	}
	n.seenTxsMu.Unlock()

	if n.OnTxHashReceived != nil {
		n.OnTxHashReceived(wanted)
	}

	n.hashesRequested += uint64(len(wanted))
	if n.hashesRequested%500 == 0 {
		fmt.Printf("[L1] Chain %d: requested %d tx hashes total\n", n.chainID.Uint64(), n.hashesRequested)
	}

	req := &GetPooledTransactionsPacket{
		RequestId: uint64(time.Now().UnixNano()),
		Hashes:    wanted,
	}

	if err := p2p.Send(ep.rw, GetPooledTransactionsMsg, req); err != nil {
		fmt.Printf("[DevP2P] Failed to request txs from %s: %v\n", ep.id, err)
	}
}

func (n *DevP2PNode) handleGetPooledTransactions(ep *ethPeer, packet *GetPooledTransactionsPacket) {
	var txs []*types.Transaction

	n.txPoolMu.RLock()
	for _, hash := range packet.Hashes {
		if tx, ok := n.txPool[hash]; ok {
			txs = append(txs, tx)
		}
	}
	n.txPoolMu.RUnlock()

	resp := &PooledTransactionsPacket{
		RequestId:    packet.RequestId,
		Transactions: txs,
	}

	p2p.Send(ep.rw, PooledTransactionsMsg, resp)
}

func (n *DevP2PNode) handlePooledTransactions(ep *ethPeer, packet *PooledTransactionsPacket) {
	if len(packet.Transactions) == 0 {
		return
	}

	ep.mu.Lock()
	for _, tx := range packet.Transactions {
		ep.knownTxs[tx.Hash()] = true
	}
	ep.mu.Unlock()

	for _, tx := range packet.Transactions {
		data := tx.Data()
		if len(data) >= 4 && data[0] == ZEAMMagic0 && data[1] == ZEAMMagic1 {
			msgType := data[3]
			switch msgType {
			case ZEAMMsgTypePriceGossip:

				if n.OnPriceGossip != nil {
					n.OnPriceGossip(data[4:])
				}
			default:

				hash := tx.Hash()
				fmt.Printf("[DevP2P] ZEAM TX: %s (version=0x%02x, type=0x%02x, %d bytes)\n",
					hash.Hex()[:18], data[2], msgType, len(data))
				if n.OnZEAMMessage != nil {
					n.OnZEAMMessage(tx, data)
				}
			}
		}
	}

	if n.OnTxReceived != nil {
		n.OnTxReceived(packet.Transactions)
	}

	n.fullTxReceived += uint64(len(packet.Transactions))
	if n.fullTxReceived%100 == 0 {
		fmt.Printf("[L1] Received %d full txs (chain %d)\n", n.fullTxReceived, n.chainID.Uint64())
	}
}

func (n *DevP2PNode) handlePooledTransactionsRawBytes(ep *ethPeer, payload []byte) {

	var decoded struct {
		RequestId uint64
		TxBytes   []rlp.RawValue
	}
	if err := rlp.DecodeBytes(payload, &decoded); err != nil {

		var txBytes []rlp.RawValue
		if err := rlp.DecodeBytes(payload, &txBytes); err != nil {
			return
		}
		decoded.TxBytes = txBytes
	}

	var validTxs []*types.Transaction
	for _, txRaw := range decoded.TxBytes {
		var tx types.Transaction
		if err := rlp.DecodeBytes(txRaw, &tx); err != nil {

			continue
		}
		validTxs = append(validTxs, &tx)
	}

	if len(validTxs) == 0 {
		return
	}

	ep.mu.Lock()
	for _, tx := range validTxs {
		ep.knownTxs[tx.Hash()] = true
	}
	ep.mu.Unlock()

	for _, tx := range validTxs {
		data := tx.Data()
		if len(data) >= 4 && data[0] == ZEAMMagic0 && data[1] == ZEAMMagic1 {
			msgType := data[3]
			if msgType == ZEAMMsgTypePriceGossip && n.OnPriceGossip != nil {
				n.OnPriceGossip(data[4:])
			} else if n.OnZEAMMessage != nil {
				n.OnZEAMMessage(tx, data)
			}
		}
	}

	if n.OnTxReceived != nil {
		n.OnTxReceived(validTxs)
	}

	n.fullTxReceived += uint64(len(validTxs))
	if n.fullTxReceived%100 == 0 {
		fmt.Printf("[L1] Received %d full txs (chain %d, %d from raw decode)\n",
			n.fullTxReceived, n.chainID.Uint64(), len(validTxs))
	}
}

func (n *DevP2PNode) removePeer(peerID string) {
	n.peersMu.Lock()
	delete(n.peers, peerID)
	n.peersMu.Unlock()

	if n.OnPeerDrop != nil {
		n.OnPeerDrop(peerID)
	}

	fmt.Printf("[DevP2P] Peer disconnected: %s\n", peerID)
}

func (n *DevP2PNode) BroadcastZEAM(data []byte) (common.Hash, error) {
	if !n.running {
		return common.Hash{}, errors.New("node not running")
	}

	nonce := n.nonceOffset + uint64(time.Now().UnixNano()%1000000)

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   n.chainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(0),
		GasFeeCap: big.NewInt(1000000000),
		Gas:       0,
		To:        &n.address,
		Value:     big.NewInt(0),
		Data:      data,
	})

	signer := types.NewLondonSigner(n.chainID)
	signed, err := types.SignTx(tx, signer, n.privateKey)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to sign tx: %w", err)
	}

	hash := signed.Hash()

	n.txPoolMu.Lock()
	n.txPool[hash] = signed
	n.txPoolMu.Unlock()

	n.seenTxsMu.Lock()
	n.seenTxs[hash] = true
	n.seenTxsMu.Unlock()

	n.peersMu.RLock()
	peers := make([]*ethPeer, 0, len(n.peers))
	for _, ep := range n.peers {
		peers = append(peers, ep)
	}
	n.peersMu.RUnlock()

	txBytes, _ := signed.MarshalBinary()
	announcement := &NewPooledTransactionHashesPacket68{
		Types:  []byte{byte(signed.Type())},
		Sizes:  []uint32{uint32(len(txBytes))},
		Hashes: []common.Hash{hash},
	}

	for _, ep := range peers {
		ep.mu.Lock()
		if !ep.knownTxs[hash] {
			ep.knownTxs[hash] = true
			go p2p.Send(ep.rw, NewPooledTransactionHashesMsg, announcement)
		}
		ep.mu.Unlock()
	}

	fmt.Printf("[DevP2P] Broadcast ZEAM TX: %s (nonce=%d, %d bytes, %d peers)\n",
		hash.Hex(), nonce, len(data), len(peers))

	return hash, nil
}

func (n *DevP2PNode) BroadcastZEAMBatchFast(payloads [][]byte) ([]common.Hash, error) {
	return n.BroadcastZEAMBatchFastMultiKey(payloads, []*ecdsa.PrivateKey{n.privateKey})
}

func (n *DevP2PNode) BroadcastZEAMBatchFastMultiKey(payloads [][]byte, keys []*ecdsa.PrivateKey) ([]common.Hash, error) {
	if !n.running {
		return nil, errors.New("node not running")
	}
	if len(keys) == 0 {
		keys = []*ecdsa.PrivateKey{n.privateKey}
	}

	count := len(payloads)
	numKeys := len(keys)

	type signResult struct {
		tx   *types.Transaction
		hash common.Hash
		idx  int
	}

	results := make(chan signResult, count)
	var wg sync.WaitGroup

	payloadsPerKey := (count + numKeys - 1) / numKeys

	for k, key := range keys {
		startIdx := k * payloadsPerKey
		endIdx := startIdx + payloadsPerKey
		if endIdx > count {
			endIdx = count
		}
		if startIdx >= count {
			break
		}

		wg.Add(1)
		go func(key *ecdsa.PrivateKey, keyIdx, start, end int) {
			defer wg.Done()

			signer := types.NewLondonSigner(n.chainID)
			addr := crypto.PubkeyToAddress(key.PublicKey)
			baseNonce := n.nonceOffset + uint64(time.Now().UnixNano()) + uint64(keyIdx*1000000)

			for i := start; i < end; i++ {
				data := payloads[i]
				tx := types.NewTx(&types.DynamicFeeTx{
					ChainID:   n.chainID,
					Nonce:     baseNonce + uint64(i-start),
					GasTipCap: big.NewInt(1000),
					GasFeeCap: big.NewInt(1000),
					Gas:       uint64(21000 + len(data)*16),
					To:        &addr,
					Value:     big.NewInt(0),
					Data:      data,
				})

				signed, err := types.SignTx(tx, signer, key)
				if err != nil {
					continue
				}

				results <- signResult{tx: signed, hash: signed.Hash(), idx: i}
			}
		}(key, k, startIdx, endIdx)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	txs := make([]*types.Transaction, 0, count)
	hashes := make([]common.Hash, 0, count)
	for r := range results {
		txs = append(txs, r.tx)
		hashes = append(hashes, r.hash)
	}

	n.txPoolMu.Lock()
	for _, tx := range txs {
		n.txPool[tx.Hash()] = tx
	}
	n.txPoolMu.Unlock()

	n.seenTxsMu.Lock()
	for _, h := range hashes {
		n.seenTxs[h] = true
	}
	n.seenTxsMu.Unlock()

	n.peersMu.RLock()
	peers := make([]*ethPeer, 0, len(n.peers))
	for _, ep := range n.peers {
		peers = append(peers, ep)
	}
	n.peersMu.RUnlock()

	for _, ep := range peers {

		types := make([]byte, len(txs))
		sizes := make([]uint32, len(txs))
		hashList := make([]common.Hash, 0, len(txs))

		ep.mu.Lock()
		for i, tx := range txs {
			h := tx.Hash()
			if !ep.knownTxs[h] {
				ep.knownTxs[h] = true
				types[i] = byte(tx.Type())
				txBytes, _ := tx.MarshalBinary()
				sizes[i] = uint32(len(txBytes))
				hashList = append(hashList, h)
			}
		}
		ep.mu.Unlock()

		if len(hashList) > 0 {
			announcement := &NewPooledTransactionHashesPacket68{
				Types:  types[:len(hashList)],
				Sizes:  sizes[:len(hashList)],
				Hashes: hashList,
			}
			go p2p.Send(ep.rw, NewPooledTransactionHashesMsg, announcement)
		}
	}

	return hashes, nil
}

func (n *DevP2PNode) BroadcastRawTx(tx *types.Transaction) error {
	if !n.running {
		return errors.New("node not running")
	}

	hash := tx.Hash()

	n.txPoolMu.Lock()
	n.txPool[hash] = tx
	n.txPoolMu.Unlock()

	n.seenTxsMu.Lock()
	n.seenTxs[hash] = true
	n.seenTxsMu.Unlock()

	n.peersMu.RLock()
	for _, ep := range n.peers {
		ep.mu.Lock()
		if !ep.knownTxs[hash] {
			ep.knownTxs[hash] = true
			go p2p.Send(ep.rw, TransactionsMsg, []*types.Transaction{tx})
		}
		ep.mu.Unlock()
	}
	n.peersMu.RUnlock()

	return nil
}

func (n *DevP2PNode) PeerCount() int {
	n.peersMu.RLock()
	defer n.peersMu.RUnlock()
	return len(n.peers)
}

func (n *DevP2PNode) Peers() []string {
	n.peersMu.RLock()
	defer n.peersMu.RUnlock()

	ids := make([]string, 0, len(n.peers))
	for id := range n.peers {
		ids = append(ids, id)
	}
	return ids
}

func (n *DevP2PNode) Address() common.Address {
	return n.address
}

func (n *DevP2PNode) AddPeer(enodeURL string) error {
	node, err := enode.Parse(enode.ValidSchemes, enodeURL)
	if err != nil {
		return fmt.Errorf("invalid enode: %w", err)
	}

	n.server.AddPeer(node)
	return nil
}

func (n *DevP2PNode) Stats() map[string]interface{} {
	n.txPoolMu.RLock()
	txCount := len(n.txPool)
	n.txPoolMu.RUnlock()

	n.seenTxsMu.RLock()
	seenCount := len(n.seenTxs)
	n.seenTxsMu.RUnlock()

	return map[string]interface{}{
		"running":     n.running,
		"address":     n.address.Hex(),
		"network_id":  n.networkID,
		"peer_count":  n.PeerCount(),
		"peers":       n.Peers(),
		"tx_pool":     txCount,
		"seen_txs":    seenCount,
		"nonce_offset": n.nonceOffset,
		"enode":       n.server.Self().URLv4(),
	}
}

func (n *DevP2PNode) CleanupTxPool(maxAge time.Duration) {

	n.txPoolMu.Lock()
	if len(n.txPool) > 10000 {
		n.txPool = make(map[common.Hash]*types.Transaction)
	}
	n.txPoolMu.Unlock()

	n.seenTxsMu.Lock()
	if len(n.seenTxs) > 100000 {
		n.seenTxs = make(map[common.Hash]bool)
	}
	n.seenTxsMu.Unlock()
}

func (n *DevP2PNode) DialPeer(addr string) error {

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}

	_ = host
	_ = port

	return errors.New("direct dial requires peer's node ID - use AddPeer with enode URL instead")
}

func encodeZEAMMessage(version byte, msgType byte, payload []byte) []byte {
	msg := make([]byte, 4+len(payload))
	msg[0] = ZEAMMagic0
	msg[1] = ZEAMMagic1
	msg[2] = version
	msg[3] = msgType
	copy(msg[4:], payload)
	return msg
}

func (n *DevP2PNode) BroadcastPriceGossip(priceData []byte) (common.Hash, error) {
	data := encodeZEAMMessage(0x01, ZEAMMsgTypePriceGossip, priceData)
	return n.BroadcastZEAM(data)
}

func hexShort(data []byte) string {
	if len(data) > 16 {
		return hex.EncodeToString(data[:16]) + "..."
	}
	return hex.EncodeToString(data)
}
