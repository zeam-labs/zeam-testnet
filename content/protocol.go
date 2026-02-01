package content

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

const (

	ContentProtocolID = protocol.ID("/zeam/content/1.0.0")

	ContentTopicName = "/zeam/content/announce"

	maxWireChunkSize = 4 * 1024 * 1024

	streamTimeout = 10 * time.Second
)

const (
	MsgChunkRequest  byte = 0x01
	MsgChunkResponse byte = 0x02
	MsgNotFound      byte = 0x03
)

type ContentAnnounce struct {
	Root   ContentID
	Size   uint64
	PeerID peer.ID
}

func (ca *ContentAnnounce) encode() []byte {
	buf := make([]byte, 40)
	copy(buf[0:32], ca.Root[:])
	binary.BigEndian.PutUint64(buf[32:40], ca.Size)
	return buf
}

func decodeContentAnnounce(data []byte) (*ContentAnnounce, error) {
	if len(data) < 40 {
		return nil, fmt.Errorf("announce too short: %d bytes", len(data))
	}
	ca := &ContentAnnounce{}
	copy(ca.Root[:], data[0:32])
	ca.Size = binary.BigEndian.Uint64(data[32:40])
	return ca, nil
}

type ContentProtocol struct {
	host      host.Host
	store     *Store
	dag       *DAGBuilder
	providers *ProviderRegistry

	topic *pubsub.Topic
	sub   *pubsub.Subscription

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewContentProtocol(h host.Host, store *Store, dag *DAGBuilder, providers *ProviderRegistry) *ContentProtocol {
	ctx, cancel := context.WithCancel(context.Background())
	return &ContentProtocol{
		host:      h,
		store:     store,
		dag:       dag,
		providers: providers,
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (cp *ContentProtocol) Start(topic *pubsub.Topic, sub *pubsub.Subscription) error {
	cp.topic = topic
	cp.sub = sub

	if cp.host != nil {
		cp.host.SetStreamHandler(ContentProtocolID, cp.handleStream)
	}

	if cp.sub != nil {
		cp.wg.Add(1)
		go cp.listenAnnouncements()
	}

	cp.wg.Add(1)
	go cp.pruneLoop()

	return nil
}

func (cp *ContentProtocol) handleStream(s network.Stream) {
	defer s.Close()
	s.SetDeadline(time.Now().Add(streamTimeout))

	var msgType [1]byte
	if _, err := io.ReadFull(s, msgType[:]); err != nil {
		return
	}

	if msgType[0] != MsgChunkRequest {
		return
	}

	var id ContentID
	if _, err := io.ReadFull(s, id[:]); err != nil {
		return
	}

	data, ok := cp.store.Get(id)
	if !ok {
		s.Write([]byte{MsgNotFound})
		return
	}

	resp := make([]byte, 1+32+4+len(data))
	resp[0] = MsgChunkResponse
	copy(resp[1:33], id[:])
	binary.BigEndian.PutUint32(resp[33:37], uint32(len(data)))
	copy(resp[37:], data)
	s.Write(resp)
}

func (cp *ContentProtocol) RequestChunk(ctx context.Context, peerID peer.ID, id ContentID) ([]byte, error) {
	if cp.host == nil {
		return nil, fmt.Errorf("no libp2p host")
	}

	s, err := cp.host.NewStream(ctx, peerID, ContentProtocolID)
	if err != nil {
		return nil, fmt.Errorf("open stream to %s: %w", peerID, err)
	}
	defer s.Close()
	s.SetDeadline(time.Now().Add(streamTimeout))

	req := make([]byte, 33)
	req[0] = MsgChunkRequest
	copy(req[1:], id[:])
	if _, err := s.Write(req); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	var msgType [1]byte
	if _, err := io.ReadFull(s, msgType[:]); err != nil {
		return nil, fmt.Errorf("read response type: %w", err)
	}

	if msgType[0] == MsgNotFound {
		return nil, fmt.Errorf("chunk %s not found on peer %s", id.Hex(), peerID)
	}

	if msgType[0] != MsgChunkResponse {
		return nil, fmt.Errorf("unexpected response type: 0x%02x", msgType[0])
	}

	var header [36]byte
	if _, err := io.ReadFull(s, header[:]); err != nil {
		return nil, fmt.Errorf("read response header: %w", err)
	}

	dataLen := binary.BigEndian.Uint32(header[32:36])
	if dataLen > maxWireChunkSize {
		return nil, fmt.Errorf("chunk too large: %d bytes", dataLen)
	}

	data := make([]byte, dataLen)
	if _, err := io.ReadFull(s, data); err != nil {
		return nil, fmt.Errorf("read chunk data: %w", err)
	}

	return data, nil
}

func (cp *ContentProtocol) Announce(root ContentID, size uint64) error {
	if cp.topic == nil {
		return fmt.Errorf("not connected to gossip")
	}
	ann := &ContentAnnounce{Root: root, Size: size}
	return cp.topic.Publish(cp.ctx, ann.encode())
}

func (cp *ContentProtocol) FetchContent(ctx context.Context, root ContentID) ([]byte, error) {

	if data, err := cp.dag.Get(root); err == nil {
		return data, nil
	}

	providers := cp.providers.Get(root)
	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers for %s", root.Hex())
	}

	if !cp.store.Has(root) {
		for _, p := range providers {
			data, err := cp.RequestChunk(ctx, p.PeerID, root)
			if err == nil {
				cp.store.PutWithID(root, data)
				break
			}
		}
	}

	missing, err := cp.dag.MissingChunks(root)
	if err != nil {
		return nil, fmt.Errorf("cannot determine missing chunks: %w", err)
	}

	for _, chunkID := range missing {
		fetched := false
		for _, p := range providers {
			data, err := cp.RequestChunk(ctx, p.PeerID, chunkID)
			if err == nil {
				cp.store.PutWithID(chunkID, data)
				fetched = true
				break
			}
		}
		if !fetched {
			return nil, fmt.Errorf("could not fetch chunk %s from any provider", chunkID.Hex())
		}
	}

	return cp.dag.Get(root)
}

func (cp *ContentProtocol) listenAnnouncements() {
	defer cp.wg.Done()
	for {
		msg, err := cp.sub.Next(cp.ctx)
		if err != nil {
			if cp.ctx.Err() != nil {
				return
			}
			continue
		}

		if cp.host != nil && msg.ReceivedFrom == cp.host.ID() {
			continue
		}

		ann, err := decodeContentAnnounce(msg.Data)
		if err != nil {
			continue
		}
		ann.PeerID = msg.ReceivedFrom

		cp.providers.Add(ann.Root, ann.PeerID, ann.Size)

		fmt.Printf("[Content] Provider %s announced %s (%d bytes)\n",
			ann.PeerID.ShortString(), ann.Root.Hex()[:16], ann.Size)
	}
}

func (cp *ContentProtocol) pruneLoop() {
	defer cp.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-cp.ctx.Done():
			return
		case <-ticker.C:
			cp.providers.Prune()
		}
	}
}

func (cp *ContentProtocol) Stop() {
	cp.cancel()
	if cp.host != nil {
		cp.host.RemoveStreamHandler(ContentProtocolID)
	}
	if cp.sub != nil {
		cp.sub.Cancel()
	}
	if cp.topic != nil {
		cp.topic.Close()
	}
	cp.wg.Wait()
}
