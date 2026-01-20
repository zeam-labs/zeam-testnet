

package node

import (
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"
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
)


type DevP2PNode struct {
	server     *p2p.Server
	privateKey *ecdsa.PrivateKey
	address    common.Address

	
	networkID   uint64
	chainID     *big.Int
	genesisHash common.Hash
	forkID      forkid.ID

	
	currentHead common.Hash
	headMu      sync.RWMutex

	
	nonceOffset uint64

	
	peers   map[string]*ethPeer
	peersMu sync.RWMutex

	
	txPool     map[common.Hash]*types.Transaction 
	txPoolMu   sync.RWMutex
	seenTxs    map[common.Hash]bool 
	seenTxsMu  sync.RWMutex

	
	OnZEAMMessage    func(tx *types.Transaction, data []byte)
	OnTxHashReceived func(hashes []common.Hash) 
	OnPeerConnect    func(peerID string)
	OnPeerDrop       func(peerID string)

	
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}


type ethPeer struct {
	id       string
	peer     *p2p.Peer
	rw       p2p.MsgReadWriter
	version  uint
	knownTxs map[common.Hash]bool
	mu       sync.RWMutex
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
	ListenAddr  string        
	MaxPeers    int
	Bootnodes   []*enode.Node
	NonceOffset uint64        
}


func SepoliaConfig(privateKey *ecdsa.PrivateKey) *DevP2PConfig {
	return &DevP2PConfig{
		PrivateKey:  privateKey,
		NetworkID:   11155111,
		ChainID:     big.NewInt(11155111),
		GenesisHash: common.HexToHash("0x25a5cc106eea7138acab33231d7160d69cb777ee0c2c553fcddf5138993e6dd9"),
		ForkID: forkid.ID{
			Hash: [4]byte{0xed, 0x88, 0xb5, 0xfd}, 
			Next: 1760427360,                       
		},
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


func NewDevP2PNode(cfg *DevP2PConfig) (*DevP2PNode, error) {
	if cfg.PrivateKey == nil {
		return nil, errors.New("private key required")
	}

	addr := crypto.PubkeyToAddress(cfg.PrivateKey.PublicKey)

	node := &DevP2PNode{
		privateKey:  cfg.PrivateKey,
		address:     addr,
		networkID:   cfg.NetworkID,
		chainID:     cfg.ChainID,
		genesisHash: cfg.GenesisHash,
		forkID:      cfg.ForkID,
		nonceOffset: cfg.NonceOffset,
		peers:       make(map[string]*ethPeer),
		txPool:      make(map[common.Hash]*types.Transaction),
		seenTxs:     make(map[common.Hash]bool),
		stopCh:      make(chan struct{}),
	}

	
	proto := p2p.Protocol{
		Name:    protocolName,
		Version: ETH68,
		Length:  17, 
		Run:     node.runPeer,
		NodeInfo: func() interface{} {
			return &ethNodeInfo{
				Network: cfg.NetworkID,
				Genesis: cfg.GenesisHash,
				Config:  nil,
				Head:    cfg.GenesisHash,
			}
		},
		PeerInfo: func(id enode.ID) interface{} {
			return nil
		},
	}

	
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

	return node, nil
}

type ethNodeInfo struct {
	Network uint64      `json:"network"`
	Genesis common.Hash `json:"genesis"`
	Config  interface{} `json:"config"`
	Head    common.Hash `json:"head"`
}


func (n *DevP2PNode) Start() error {
	if n.running {
		return errors.New("already running")
	}

	if err := n.server.Start(); err != nil {
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
	
	
	td := new(big.Int)
	td.SetString("17000000000000000", 10)

	
	head := n.genesisHash
	n.headMu.RLock()
	if n.currentHead != (common.Hash{}) {
		head = n.currentHead
	}
	n.headMu.RUnlock()

	status := &StatusPacket{
		ProtocolVersion: ETH68,
		NetworkID:       n.networkID,
		TD:              td,
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

	
	if peerStatus.NetworkID != n.networkID {
		return fmt.Errorf("network mismatch: %d vs %d", peerStatus.NetworkID, n.networkID)
	}
	if peerStatus.Genesis != n.genesisHash {
		return fmt.Errorf("genesis mismatch")
	}

	
	n.headMu.Lock()
	n.currentHead = peerStatus.Head
	n.headMu.Unlock()

	ep.version = uint(peerStatus.ProtocolVersion)
	return nil
}


func (n *DevP2PNode) handleMsg(ep *ethPeer, msg p2p.Msg) error {
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
		
		var packet PooledTransactionsPacket
		if err := msg.Decode(&packet); err != nil {
			
			fmt.Printf("[DevP2P] Ignoring pooled-tx decode error from %s (likely blob tx)\n", ep.id)
			return nil
		}
		n.handlePooledTransactions(ep, &packet)

	case NewBlockHashesMsg, NewBlockMsg, GetBlockHeadersMsg, BlockHeadersMsg, GetBlockBodiesMsg, BlockBodiesMsg:
		
		return nil

	default:
		
		return nil
	}

	return nil
}


func (n *DevP2PNode) handleTransactions(ep *ethPeer, txs []*types.Transaction) {
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

		
		data := tx.Data()
		if len(data) >= 2 && data[0] == ZEAMMagic0 && data[1] == ZEAMMagic1 {
			fmt.Printf("[DevP2P] ZEAM TX: %s (version=0x%02x, %d bytes)\n",
				hash.Hex()[:18], data[2], len(data))

			if n.OnZEAMMessage != nil {
				n.OnZEAMMessage(tx, data)
			}
		}
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
	n.handleTransactions(ep, packet.Transactions)
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
		GasTipCap: big.NewInt(1000),    
		GasFeeCap: big.NewInt(1000),    
		Gas:       uint64(21000 + len(data)*16), 
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


func hexShort(data []byte) string {
	if len(data) > 16 {
		return hex.EncodeToString(data[:16]) + "..."
	}
	return hex.EncodeToString(data)
}
