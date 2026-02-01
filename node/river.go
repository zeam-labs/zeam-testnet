package node

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type NetworkEventType int

const (
	EventTxHashes    NetworkEventType = iota
	EventTxFull
	EventZEAMMessage
	EventL1Block
	EventL2Block
	EventPeerConnect
	EventPeerDrop
)

type NetworkEvent struct {
	Type      NetworkEventType
	ChainID   uint64
	Timestamp time.Time

	Hashes []common.Hash

	Txs []*types.Transaction

	ZEAMData []byte
	ZEAMTx   *types.Transaction

	L1BlockHash   common.Hash
	L1BlockNumber uint64
	L2Block       *L2BlockGossip

	PeerID    string
	NetworkID uint64
}

type BroadcastResult struct {
	TxHash common.Hash
	Err    error
}

type NetworkStats struct {
	L1Peers     int
	L2Peers     map[uint64]int
	MempoolRate float64
	LastTxHash  time.Time
	RelayAddrs  int
	DirectConns int
}

type NetworkFeed interface {

	Subscribe(bufSize int) *FeedSubscription

	Broadcast(payload []byte) <-chan BroadcastResult

	BroadcastTx(tx *types.Transaction) <-chan BroadcastResult

	Stats() NetworkStats

	Start() error
	Stop()
}

type FeedSubscription struct {
	C  <-chan NetworkEvent
	id uint64
	ch chan NetworkEvent
	f  *EventFanOut
}

func (s *FeedSubscription) Unsubscribe() {
	s.f.remove(s.id)
}

type EventFanOut struct {
	mu     sync.RWMutex
	subs   map[uint64]*FeedSubscription
	nextID atomic.Uint64
}

func (f *EventFanOut) Init() {
	f.mu.Lock()
	if f.subs == nil {
		f.subs = make(map[uint64]*FeedSubscription)
	}
	f.mu.Unlock()
}

func newEventFanOut() *EventFanOut {
	return &EventFanOut{
		subs: make(map[uint64]*FeedSubscription),
	}
}

func (f *EventFanOut) Subscribe(bufSize int) *FeedSubscription {
	id := f.nextID.Add(1)
	ch := make(chan NetworkEvent, bufSize)
	sub := &FeedSubscription{
		C:  ch,
		id: id,
		ch: ch,
		f:  f,
	}
	f.mu.Lock()
	f.subs[id] = sub
	f.mu.Unlock()
	return sub
}

func (f *EventFanOut) remove(id uint64) {
	f.mu.Lock()
	if sub, ok := f.subs[id]; ok {
		close(sub.ch)
		delete(f.subs, id)
	}
	f.mu.Unlock()
}

func (f *EventFanOut) Emit(event NetworkEvent) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, sub := range f.subs {
		select {
		case sub.ch <- event:
		default:

		}
	}
}

func (f *EventFanOut) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, sub := range f.subs {
		close(sub.ch)
		delete(f.subs, id)
	}
}
