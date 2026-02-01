package node

import (
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type P2PTransport struct {
	node       *DevP2PNode
	privateKey *ecdsa.PrivateKey
	address    common.Address

	cache *ForkCache

	OnZEAMBlock    func(forkID [32]byte, data []byte)
	OnZEAMGenesis  func(forkID [32]byte, data []byte)
	OnZEAMCrossMsg func(fromFork, toFork [32]byte, encrypted []byte)
	OnLLMQuery     func(data []byte) []byte
	OnLLMResponse  func(data []byte)
	OnForkRequest  func(forkID [32]byte, fromHeight uint64)
	OnForkSync     func(forkID [32]byte, blocks []*ForkBlock)

	OnCollapseInput  func(queryHash [32]byte, input []byte)
	OnCollapseOutput func(queryHash [32]byte, txHash common.Hash, semantics []byte)

	msgsSent     uint64
	msgsReceived uint64
	statsMu      sync.RWMutex

	running bool
}

const (
	ZEAMVersion = 0x04

	MsgForkBlock    = 0x01
	MsgForkGenesis  = 0x02
	MsgCrossMsg     = 0x03
	MsgPing         = 0x04
	MsgLLMQuery     = 0x05
	MsgLLMResponse  = 0x06
	MsgForkRequest  = 0x07
	MsgForkSync     = 0x08
	MsgCollapseIn   = 0x09
	MsgCollapseOut  = 0x0A
)

type ForkBlock struct {
	ForkID    [32]byte
	Height    uint64
	Timestamp uint64
	PrevHash  [32]byte
	StateRoot [32]byte
	Payload   []byte
	Signature []byte
}

type ForkCache struct {
	blocks map[[32]byte]map[uint64]*ForkBlock
	mu     sync.RWMutex
}

func NewForkCache() *ForkCache {
	return &ForkCache{
		blocks: make(map[[32]byte]map[uint64]*ForkBlock),
	}
}

func (c *ForkCache) Add(block *ForkBlock) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.blocks[block.ForkID] == nil {
		c.blocks[block.ForkID] = make(map[uint64]*ForkBlock)
	}
	c.blocks[block.ForkID][block.Height] = block
}

func (c *ForkCache) Get(forkID [32]byte, height uint64) *ForkBlock {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.blocks[forkID] == nil {
		return nil
	}
	return c.blocks[forkID][height]
}

func (c *ForkCache) GetForkBlocks(forkID [32]byte) []*ForkBlock {
	c.mu.RLock()
	defer c.mu.RUnlock()

	forkBlocks := c.blocks[forkID]
	if forkBlocks == nil {
		return nil
	}

	var maxHeight uint64
	for h := range forkBlocks {
		if h > maxHeight {
			maxHeight = h
		}
	}

	result := make([]*ForkBlock, 0, len(forkBlocks))
	for h := uint64(0); h <= maxHeight; h++ {
		if b := forkBlocks[h]; b != nil {
			result = append(result, b)
		}
	}
	return result
}

func (c *ForkCache) LatestHeight(forkID [32]byte) uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var max uint64
	for h := range c.blocks[forkID] {
		if h > max {
			max = h
		}
	}
	return max
}

func (c *ForkCache) ForkCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.blocks)
}

func (c *ForkCache) BlockCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	count := 0
	for _, blocks := range c.blocks {
		count += len(blocks)
	}
	return count
}

func NewP2PTransport(privateKey *ecdsa.PrivateKey, config *DevP2PConfig) (*P2PTransport, error) {
	if privateKey == nil {
		return nil, errors.New("private key required")
	}

	if config == nil {
		config = SepoliaConfig(privateKey)
	} else {
		config.PrivateKey = privateKey
	}

	if len(config.Bootnodes) == 0 {
		config.Bootnodes = SepoliaBootnodes()
	}

	node, err := NewDevP2PNode(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create DevP2P node: %w", err)
	}

	t := &P2PTransport{
		node:       node,
		privateKey: privateKey,
		address:    crypto.PubkeyToAddress(privateKey.PublicKey),
		cache:      NewForkCache(),
	}

	node.OnZEAMMessage = t.handleZEAMMessage

	return t, nil
}

func (t *P2PTransport) Cache() *ForkCache {
	return t.cache
}

func (t *P2PTransport) Start() error {
	if t.running {
		return errors.New("already running")
	}

	if err := t.node.Start(); err != nil {
		return err
	}

	t.running = true
	fmt.Printf("[P2PTransport] Started (address: %s)\n", t.address.Hex())

	return nil
}

func (t *P2PTransport) Stop() {
	if !t.running {
		return
	}

	t.node.Stop()
	t.running = false
	fmt.Printf("[P2PTransport] Stopped\n")
}

func (t *P2PTransport) handleZEAMMessage(tx *types.Transaction, data []byte) {
	if len(data) < 4 {
		return
	}

	version := data[2]
	msgType := data[3]

	t.statsMu.Lock()
	t.msgsReceived++
	t.statsMu.Unlock()

	switch {
	case version == ZEAMVersion && msgType == MsgForkBlock:
		if len(data) >= 36 && t.OnZEAMBlock != nil {
			var forkID [32]byte
			copy(forkID[:], data[4:36])
			t.OnZEAMBlock(forkID, data[36:])
		}

	case version == ZEAMVersion && msgType == MsgForkGenesis:
		if len(data) >= 36 && t.OnZEAMGenesis != nil {
			var forkID [32]byte
			copy(forkID[:], data[4:36])
			t.OnZEAMGenesis(forkID, data[36:])
		}

	case version == ZEAMVersion && msgType == MsgCrossMsg:
		if len(data) >= 68 && t.OnZEAMCrossMsg != nil {
			var fromFork, toFork [32]byte
			copy(fromFork[:], data[4:36])
			copy(toFork[:], data[36:68])
			t.OnZEAMCrossMsg(fromFork, toFork, data[68:])
		}

	case version == ZEAMVersion && msgType == MsgLLMQuery:
		if t.OnLLMQuery != nil {
			response := t.OnLLMQuery(data[4:])
			if response != nil {

				t.BroadcastLLMResponse(response)
			}
		}

	case version == ZEAMVersion && msgType == MsgLLMResponse:
		if t.OnLLMResponse != nil {
			t.OnLLMResponse(data[4:])
		}

	case version == 0x02:
		if t.OnLLMQuery != nil {
			response := t.OnLLMQuery(data)
			if response != nil {
				t.BroadcastLLMResponse(response)
			}
		}

	case version == 0x82:
		if t.OnLLMResponse != nil {
			t.OnLLMResponse(data)
		}

	case version == ZEAMVersion && msgType == MsgForkRequest:

		if len(data) >= 44 {
			var forkID [32]byte
			copy(forkID[:], data[4:36])
			fromHeight := uint64(0)
			for i := 0; i < 8; i++ {
				fromHeight |= uint64(data[36+i]) << (56 - i*8)
			}

			blocks := t.cache.GetForkBlocks(forkID)
			if len(blocks) > 0 {

				var toSend []*ForkBlock
				for _, b := range blocks {
					if b.Height >= fromHeight {
						toSend = append(toSend, b)
					}
				}
				if len(toSend) > 0 {
					t.BroadcastForkSync(forkID, toSend)
				}
			}

			if t.OnForkRequest != nil {
				t.OnForkRequest(forkID, fromHeight)
			}
		}

	case version == ZEAMVersion && msgType == MsgForkSync:

		if len(data) >= 40 {
			var forkID [32]byte
			copy(forkID[:], data[4:36])

			blockCount := uint32(0)
			for i := 0; i < 4; i++ {
				blockCount |= uint32(data[36+i]) << (24 - i*8)
			}

			blocks := make([]*ForkBlock, 0, blockCount)
			offset := 40
			for i := uint32(0); i < blockCount && offset < len(data); i++ {
				block, bytesRead := parseForkBlockFromSync(forkID, data[offset:])
				if block != nil {
					blocks = append(blocks, block)
					t.cache.Add(block)
					offset += bytesRead
				} else {
					break
				}
			}

			if len(blocks) > 0 {
				fmt.Printf("[P2PTransport] Received %d blocks for fork %x...\n", len(blocks), forkID[:8])
				if t.OnForkSync != nil {
					t.OnForkSync(forkID, blocks)
				}
			}
		}

	case version == ZEAMVersion && msgType == MsgCollapseIn:

		if len(data) >= 36 && t.OnCollapseInput != nil {
			var queryHash [32]byte
			copy(queryHash[:], data[4:36])
			t.OnCollapseInput(queryHash, data[36:])
		}

	case version == ZEAMVersion && msgType == MsgCollapseOut:

		if len(data) >= 68 && t.OnCollapseOutput != nil {
			var queryHash [32]byte
			copy(queryHash[:], data[4:36])
			txHash := common.BytesToHash(data[36:68])
			t.OnCollapseOutput(queryHash, txHash, data[68:])
		}
	}
}

func (t *P2PTransport) BroadcastForkBlock(forkID [32]byte, blockData []byte) (common.Hash, error) {
	msg := t.encodeMessage(MsgForkBlock, forkID[:], blockData)
	return t.broadcast(msg)
}

func (t *P2PTransport) BroadcastForkGenesis(forkID [32]byte, genesisData []byte) (common.Hash, error) {
	msg := t.encodeMessage(MsgForkGenesis, forkID[:], genesisData)
	return t.broadcast(msg)
}

func (t *P2PTransport) BroadcastCrossMessage(fromFork, toFork [32]byte, encrypted []byte) (common.Hash, error) {
	header := make([]byte, 64)
	copy(header[:32], fromFork[:])
	copy(header[32:], toFork[:])
	msg := t.encodeMessage(MsgCrossMsg, header, encrypted)
	return t.broadcast(msg)
}

func (t *P2PTransport) BroadcastLLMQuery(query []byte) (common.Hash, error) {
	msg := t.encodeMessage(MsgLLMQuery, nil, query)
	return t.broadcast(msg)
}

func (t *P2PTransport) BroadcastLLMResponse(response []byte) (common.Hash, error) {
	msg := t.encodeMessage(MsgLLMResponse, nil, response)
	return t.broadcast(msg)
}

func (t *P2PTransport) BroadcastCollapseInput(queryHash [32]byte, input []byte) (common.Hash, error) {
	msg := t.encodeMessage(MsgCollapseIn, queryHash[:], input)
	return t.broadcast(msg)
}

func (t *P2PTransport) BroadcastCollapseOutput(queryHash [32]byte, txHash common.Hash, semantics []byte) (common.Hash, error) {
	header := make([]byte, 64)
	copy(header[:32], queryHash[:])
	copy(header[32:], txHash.Bytes())
	msg := t.encodeMessage(MsgCollapseOut, header, semantics)
	return t.broadcast(msg)
}

func (t *P2PTransport) BroadcastForkRequest(forkID [32]byte, fromHeight uint64) (common.Hash, error) {

	header := make([]byte, 40)
	copy(header[:32], forkID[:])
	for i := 0; i < 8; i++ {
		header[32+i] = byte(fromHeight >> (56 - i*8))
	}
	msg := t.encodeMessage(MsgForkRequest, header, nil)
	return t.broadcast(msg)
}

func (t *P2PTransport) BroadcastForkSync(forkID [32]byte, blocks []*ForkBlock) (common.Hash, error) {

	header := make([]byte, 36)
	copy(header[:32], forkID[:])
	blockCount := uint32(len(blocks))
	for i := 0; i < 4; i++ {
		header[32+i] = byte(blockCount >> (24 - i*8))
	}

	var payload []byte
	for _, b := range blocks {
		payload = append(payload, encodeForkBlockForSync(b)...)
	}

	msg := t.encodeMessage(MsgForkSync, header, payload)
	return t.broadcast(msg)
}

func (t *P2PTransport) RequestForkSync(forkID [32]byte, fromHeight uint64, timeout time.Duration) ([]*ForkBlock, error) {

	_, err := t.BroadcastForkRequest(forkID, fromHeight)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[P2PTransport] Requesting fork %x... from height %d\n", forkID[:8], fromHeight)

	deadline := time.Now().Add(timeout)
	initialCount := len(t.cache.GetForkBlocks(forkID))

	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		currentBlocks := t.cache.GetForkBlocks(forkID)
		if len(currentBlocks) > initialCount {

			return currentBlocks, nil
		}
	}

	return t.cache.GetForkBlocks(forkID), nil
}

func encodeForkBlockForSync(b *ForkBlock) []byte {

	buf := make([]byte, 86+len(b.Payload)+len(b.Signature))

	for i := 0; i < 8; i++ {
		buf[i] = byte(b.Height >> (56 - i*8))
	}

	for i := 0; i < 8; i++ {
		buf[8+i] = byte(b.Timestamp >> (56 - i*8))
	}

	copy(buf[16:48], b.PrevHash[:])

	copy(buf[48:80], b.StateRoot[:])

	payloadLen := uint32(len(b.Payload))
	for i := 0; i < 4; i++ {
		buf[80+i] = byte(payloadLen >> (24 - i*8))
	}

	copy(buf[84:84+len(b.Payload)], b.Payload)

	sigOffset := 84 + len(b.Payload)
	sigLen := uint16(len(b.Signature))
	buf[sigOffset] = byte(sigLen >> 8)
	buf[sigOffset+1] = byte(sigLen)

	copy(buf[sigOffset+2:], b.Signature)

	return buf
}

func parseForkBlockFromSync(forkID [32]byte, data []byte) (*ForkBlock, int) {
	if len(data) < 86 {
		return nil, 0
	}

	b := &ForkBlock{ForkID: forkID}

	for i := 0; i < 8; i++ {
		b.Height |= uint64(data[i]) << (56 - i*8)
	}

	for i := 0; i < 8; i++ {
		b.Timestamp |= uint64(data[8+i]) << (56 - i*8)
	}

	copy(b.PrevHash[:], data[16:48])

	copy(b.StateRoot[:], data[48:80])

	payloadLen := uint32(0)
	for i := 0; i < 4; i++ {
		payloadLen |= uint32(data[80+i]) << (24 - i*8)
	}

	if len(data) < int(84+payloadLen+2) {
		return nil, 0
	}

	b.Payload = make([]byte, payloadLen)
	copy(b.Payload, data[84:84+payloadLen])

	sigOffset := 84 + int(payloadLen)
	sigLen := uint16(data[sigOffset])<<8 | uint16(data[sigOffset+1])

	if len(data) < sigOffset+2+int(sigLen) {
		return nil, 0
	}

	b.Signature = make([]byte, sigLen)
	copy(b.Signature, data[sigOffset+2:sigOffset+2+int(sigLen)])

	totalBytes := sigOffset + 2 + int(sigLen)
	return b, totalBytes
}

func (t *P2PTransport) BroadcastRaw(data []byte) (common.Hash, error) {
	return t.broadcast(data)
}

func (t *P2PTransport) encodeMessage(msgType byte, header, payload []byte) []byte {
	totalLen := 4 + len(header) + len(payload)
	msg := make([]byte, totalLen)
	msg[0] = ZEAMMagic0
	msg[1] = ZEAMMagic1
	msg[2] = ZEAMVersion
	msg[3] = msgType
	if len(header) > 0 {
		copy(msg[4:], header)
	}
	if len(payload) > 0 {
		copy(msg[4+len(header):], payload)
	}
	return msg
}

func (t *P2PTransport) broadcast(data []byte) (common.Hash, error) {
	if !t.running {
		return common.Hash{}, errors.New("transport not running")
	}

	hash, err := t.node.BroadcastZEAM(data)
	if err != nil {
		return common.Hash{}, err
	}

	t.statsMu.Lock()
	t.msgsSent++
	t.statsMu.Unlock()

	return hash, nil
}

func (t *P2PTransport) AddPeer(enodeURL string) error {
	return t.node.AddPeer(enodeURL)
}

func (t *P2PTransport) PeerCount() int {
	return t.node.PeerCount()
}

func (t *P2PTransport) Address() common.Address {
	return t.address
}

func (t *P2PTransport) Stats() map[string]interface{} {
	t.statsMu.RLock()
	sent := t.msgsSent
	received := t.msgsReceived
	t.statsMu.RUnlock()

	nodeStats := t.node.Stats()
	nodeStats["msgs_sent"] = sent
	nodeStats["msgs_received"] = received

	return nodeStats
}

func (t *P2PTransport) WaitForPeers(n int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if t.PeerCount() >= n {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %d peers (have %d)", n, t.PeerCount())
}

func (t *P2PTransport) ForkID() [32]byte {
	return crypto.Keccak256Hash(t.address.Bytes())
}

func FormatForkBlock(height uint64, parentHash [32]byte, stateRoot [32]byte, data []byte) []byte {

	payload := make([]byte, 72+len(data))

	for i := 0; i < 8; i++ {
		payload[i] = byte(height >> (56 - i*8))
	}
	copy(payload[8:40], parentHash[:])
	copy(payload[40:72], stateRoot[:])
	copy(payload[72:], data)

	return payload
}

func ParseForkBlock(payload []byte) (height uint64, parentHash, stateRoot [32]byte, data []byte, err error) {
	if len(payload) < 72 {
		return 0, [32]byte{}, [32]byte{}, nil, errors.New("payload too short")
	}

	for i := 0; i < 8; i++ {
		height |= uint64(payload[i]) << (56 - i*8)
	}
	copy(parentHash[:], payload[8:40])
	copy(stateRoot[:], payload[40:72])
	data = payload[72:]

	return height, parentHash, stateRoot, data, nil
}

func LogMessage(prefix string, data []byte) {
	if len(data) < 4 {
		fmt.Printf("[%s] Invalid message: too short\n", prefix)
		return
	}

	version := data[2]
	msgType := data[3]

	typeStr := "UNKNOWN"
	switch msgType {
	case MsgForkBlock:
		typeStr = "FORK_BLOCK"
	case MsgForkGenesis:
		typeStr = "FORK_GENESIS"
	case MsgCrossMsg:
		typeStr = "CROSS_MSG"
	case MsgPing:
		typeStr = "PING"
	case MsgLLMQuery:
		typeStr = "LLM_QUERY"
	case MsgLLMResponse:
		typeStr = "LLM_RESPONSE"
	}

	forkStr := ""
	if len(data) >= 36 {
		forkStr = hex.EncodeToString(data[4:12]) + "..."
	}

	fmt.Printf("[%s] v=0x%02x type=%s fork=%s len=%d\n",
		prefix, version, typeStr, forkStr, len(data))
}
