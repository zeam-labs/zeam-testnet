package rewards

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"zeam/content"
)

const (

	ChallengeProtocolID = protocol.ID("/zeam/storage-challenge/1.0.0")

	MsgChallenge         byte = 0x10
	MsgChallengeResponse byte = 0x11
)

type ChallengeManager struct {
	mu sync.RWMutex

	host  host.Host
	store *content.Store

	pendingOut map[[32]byte]*ChallengeRecord

	completed []*ChallengeRecord

	OnChallengeComplete func(record *ChallengeRecord)

	config *Config
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewChallengeManager(h host.Host, store *content.Store, cfg *Config) *ChallengeManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &ChallengeManager{
		host:       h,
		store:      store,
		pendingOut: make(map[[32]byte]*ChallengeRecord),
		config:     cfg,
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (cm *ChallengeManager) Start() {
	if cm.host != nil {
		cm.host.SetStreamHandler(ChallengeProtocolID, cm.handleStream)
	}
}

func (cm *ChallengeManager) IssueChallenge(ctx context.Context, target peer.ID, chunkID content.ContentID) error {
	if cm.host == nil {
		return fmt.Errorf("no libp2p host")
	}

	data, ok := cm.store.Get(chunkID)
	if !ok {
		return fmt.Errorf("cannot challenge for chunk we don't have: %s", chunkID.Hex())
	}

	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}

	expectedHash := crypto.Keccak256Hash(append(data, nonce[:]...))

	record := &ChallengeRecord{
		ChunkID:      chunkID,
		Challenger:   cm.host.ID(),
		Challenged:   target,
		Nonce:        nonce,
		ExpectedHash: expectedHash,
		IssuedAt:     time.Now(),
		Deadline:     time.Now().Add(cm.config.ChallengeTimeout),
	}

	copy(record.ID[:], crypto.Keccak256(append(chunkID[:], nonce[:]...)))

	stream, err := cm.host.NewStream(ctx, target, ChallengeProtocolID)
	if err != nil {
		return fmt.Errorf("failed to open challenge stream to %s: %w", target.ShortString(), err)
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(cm.config.ChallengeTimeout))

	msg := make([]byte, 1+32+32+32)
	msg[0] = MsgChallenge
	copy(msg[1:33], record.ID[:])
	copy(msg[33:65], chunkID[:])
	copy(msg[65:97], nonce[:])

	if _, err := stream.Write(msg); err != nil {
		return fmt.Errorf("failed to send challenge: %w", err)
	}

	cm.mu.Lock()
	cm.pendingOut[record.ID] = record
	cm.mu.Unlock()

	var resp [65]byte
	if _, err := io.ReadFull(stream, resp[:]); err != nil {

		cm.markFailed(record)
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp[0] != MsgChallengeResponse {
		cm.markFailed(record)
		return fmt.Errorf("unexpected response type: 0x%02x", resp[0])
	}

	var respChallengeID [32]byte
	copy(respChallengeID[:], resp[1:33])
	if respChallengeID != record.ID {
		cm.markFailed(record)
		return fmt.Errorf("challenge ID mismatch")
	}

	var responseHash [32]byte
	copy(responseHash[:], resp[33:65])

	cm.mu.Lock()
	delete(cm.pendingOut, record.ID)
	record.Response = responseHash[:]
	record.Passed = responseHash == expectedHash
	record.Verified = true
	cm.completed = append(cm.completed, record)
	cm.mu.Unlock()

	if cm.OnChallengeComplete != nil {
		cm.OnChallengeComplete(record)
	}

	return nil
}

func (cm *ChallengeManager) handleStream(s network.Stream) {
	defer s.Close()
	s.SetDeadline(time.Now().Add(cm.config.ChallengeTimeout))

	var msgType [1]byte
	if _, err := io.ReadFull(s, msgType[:]); err != nil {
		return
	}

	if msgType[0] != MsgChallenge {
		return
	}

	var header [96]byte
	if _, err := io.ReadFull(s, header[:]); err != nil {
		return
	}

	var challengeID [32]byte
	var chunkID content.ContentID
	var nonce [32]byte
	copy(challengeID[:], header[0:32])
	copy(chunkID[:], header[32:64])
	copy(nonce[:], header[64:96])

	var responseHash [32]byte
	if data, ok := cm.store.Get(chunkID); ok {

		hash := crypto.Keccak256(append(data, nonce[:]...))
		copy(responseHash[:], hash)
	}

	resp := make([]byte, 1+32+32)
	resp[0] = MsgChallengeResponse
	copy(resp[1:33], challengeID[:])
	copy(resp[33:65], responseHash[:])

	s.Write(resp)
}

func (cm *ChallengeManager) markFailed(record *ChallengeRecord) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.pendingOut, record.ID)
	record.Passed = false
	record.Verified = true
	cm.completed = append(cm.completed, record)

	if cm.OnChallengeComplete != nil {
		go cm.OnChallengeComplete(record)
	}
}

func (cm *ChallengeManager) GetCompletedChallenges() []*ChallengeRecord {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	result := cm.completed
	cm.completed = nil
	return result
}

func (cm *ChallengeManager) PendingCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.pendingOut)
}

func (cm *ChallengeManager) Stop() {
	cm.cancel()
	if cm.host != nil {
		cm.host.RemoveStreamHandler(ChallengeProtocolID)
	}
	cm.wg.Wait()
}
