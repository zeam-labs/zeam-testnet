package node

import (
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	libp2p "github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/golang/snappy"
	ma "github.com/multiformats/go-multiaddr"
)

type OpNodeGossip struct {
	host       host.Host
	hostOwned  bool
	pubsub     *pubsub.PubSub
	privateKey *ecdsa.PrivateKey

	topics map[uint64]*pubsub.Topic
	subs   map[uint64]*pubsub.Subscription

	priceTopic *pubsub.Topic
	priceSub   *pubsub.Subscription

	OnBlockReceived func(chainID uint64, block *L2BlockGossip)
	OnPeerJoin      func(chainID uint64, peerID string)
	OnPeerLeave     func(chainID uint64, peerID string)
	OnPriceGossip   func(data []byte)

	blocksReceived map[uint64]uint64
	pricesReceived uint64
	decodeErrors   uint64
	statsMu        sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

type L2BlockGossip struct {
	ChainID     uint64
	BlockNumber uint64
	BlockHash   common.Hash
	ParentHash  common.Hash
	Timestamp   uint64

	Transactions [][]byte

	ReceivedAt time.Time
	FromPeer   string
}

type ExecutionPayloadEnvelope struct {
	ParentHash    common.Hash
	FeeRecipient  common.Address
	StateRoot     common.Hash
	ReceiptsRoot  common.Hash
	LogsBloom     []byte
	PrevRandao    common.Hash
	BlockNumber   uint64
	GasLimit      uint64
	GasUsed       uint64
	Timestamp     uint64
	ExtraData     []byte
	BaseFeePerGas []byte
	BlockHash     common.Hash
	Transactions  [][]byte
}

var opStackBootnodeENRs = map[string][]string{

	"oplabs": {
		"enode://869d07b5932f17e8490990f75a3f94195e9504ddb6b85f7189e5a9c0a8fff8b00aecf6f3ac450ecba6cdabdb5858788a94bde2b613e0f2d82e9b395355f76d1a@34.65.67.101:30305",
		"enode://2d4e7e9d48f4dd4efe9342706dd1b0024681bd4c3300d021f86fc75eab7865d4e0cbec6fbc883f011cfd6a57423e7e2f6e104baad2b744c3cafaec6bc7dc92c1@34.65.43.171:30305",
	},

	"conduit": {
		"enode://d25ce99435982b04d60c4b41ba256b84b888626db7bee45a9419382300fbe907359ae5ef250346785bff8d3b9d07cd3e017a27e2ee3cfda3bcbb0ba762ac9674@bootnode.conduit.xyz:30301",
		"enode://9d7a3efefe442351217e73b3a593bcb8efffb55b4807699972145324eab5e6b382152f8d24f6301baebbfb5ecd4127bd3faab2842c04cd432bdf50ba092f6645@34.65.109.126:30305",
	},

	"uniswap": {
		"enode://010800c668896c100e8d64abc388ac5a22a8134a96fb0107c5d0c56d79ba7225c12d9e9e012d3cc0ee2701d7f63dd45f8abf0bbcf6f3c541f91742b1d7a99355@3.134.214.169:9222",
		"enode://b97abcc7011d06299c4bc44742be4a0e631a1a2925a2992adcfe80ed86bec5ff0ddf1b90d015f2dbb5e305560e12c9873b2dad72d84d131ac4be9f2a4c74b763@52.14.30.39:9222",
		"enode://760230a662610620d6d2e4ad846a6dccbceaa4556872dfacf9cdca7c2f5b49e4c66e822ed2e8813debb5fb7391f0519b8d075e565a2a89c79a9e4092e81b3e5b@3.148.100.173:9222",
	},
}

var opStackBootnodes = map[uint64][]string{

	8453: {
		"enr:-J24QNz9lbrKbN4iSmmjtnr7SjUMk4zB7f1krHZcTZx-JRKZd0kA2gjufUROD6T3sOWDVDnFJRvqBBo62zuF-hYCohOGAYiOoEyEgmlkgnY0gmlwhAPniryHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQKNVFlCxh_B-716tTs-h1vMzZkSs1FTu_OYTNjgufplG4N0Y3CCJAaDdWRwgiQG",
		"enr:-J24QH-f1wt99sfpHy4c0QJM-NfmsIfmlLAMMcgZCUEgKG_BBYFc6FwYgaMJMQN5dsRBJApIok0jFn-9CS842lGpLmqGAYiOoDRAgmlkgnY0gmlwhLhIgb2Hb3BzdGFja4OFQgCJc2VjcDI1NmsxoQJ9FTIv8B9myn1MWaC_2lJ-sMoeCDkusCsk4BYHjjCq04N0Y3CCJAaDdWRwgiQG",
		"enr:-J24QDXyyxvQYsd0yfsN0cRr1lZ1N11zGTplMNlW4xNEc7LkPXh0NAJ9iSOVdRO95GPYAIc6xmyoCCG6_0JxdL3a0zaGAYiOoAjFgmlkgnY0gmlwhAPckbGHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQJwoS7tzwxqXSyFL7g0JM-KWVbgvjfB8JA__T7yY_cYboN0Y3CCJAaDdWRwgiQG",
		"enr:-J24QHmGyBwUZXIcsGYMaUqGGSl4CFdx9Tozu-vQCn5bHIQbR7On7dZbU61vYvfrJr30t0iahSqhc64J46MnUO2JvQaGAYiOoCKKgmlkgnY0gmlwhAPnCzSHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQINc4fSijfbNIiGhcgvwjsjxVFJHUstK9L1T8OTKUjgloN0Y3CCJAaDdWRwgiQG",
		"enr:-J24QG3ypT4xSu0gjb5PABCmVxZqBjVw9ca7pvsI8jl4KATYAnxBmfkaIuEqy9sKvDHKuNCsy57WwK9wTt2aQgcaDDyGAYiOoGAXgmlkgnY0gmlwhDbGmZaHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQIeAK_--tcLEiu7HvoUlbV52MspE0uCocsx1f_rYvRenIN0Y3CCJAaDdWRwgiQG",
	},

	10: {
		"enr:-J24QNz9lbrKbN4iSmmjtnr7SjUMk4zB7f1krHZcTZx-JRKZd0kA2gjufUROD6T3sOWDVDnFJRvqBBo62zuF-hYCohOGAYiOoEyEgmlkgnY0gmlwhAPniryHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQKNVFlCxh_B-716tTs-h1vMzZkSs1FTu_OYTNjgufplG4N0Y3CCJAaDdWRwgiQG",
		"enr:-J24QH-f1wt99sfpHy4c0QJM-NfmsIfmlLAMMcgZCUEgKG_BBYFc6FwYgaMJMQN5dsRBJApIok0jFn-9CS842lGpLmqGAYiOoDRAgmlkgnY0gmlwhLhIgb2Hb3BzdGFja4OFQgCJc2VjcDI1NmsxoQJ9FTIv8B9myn1MWaC_2lJ-sMoeCDkusCsk4BYHjjCq04N0Y3CCJAaDdWRwgiQG",
		"enr:-J24QG3ypT4xSu0gjb5PABCmVxZqBjVw9ca7pvsI8jl4KATYAnxBmfkaIuEqy9sKvDHKuNCsy57WwK9wTt2aQgcaDDyGAYiOoGAXgmlkgnY0gmlwhDbGmZaHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQIeAK_--tcLEiu7HvoUlbV52MspE0uCocsx1f_rYvRenIN0Y3CCJAaDdWRwgiQG",
	},

	7777777: {
		"enr:-J24QNz9lbrKbN4iSmmjtnr7SjUMk4zB7f1krHZcTZx-JRKZd0kA2gjufUROD6T3sOWDVDnFJRvqBBo62zuF-hYCohOGAYiOoEyEgmlkgnY0gmlwhAPniryHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQKNVFlCxh_B-716tTs-h1vMzZkSs1FTu_OYTNjgufplG4N0Y3CCJAaDdWRwgiQG",
		"enr:-J24QH-f1wt99sfpHy4c0QJM-NfmsIfmlLAMMcgZCUEgKG_BBYFc6FwYgaMJMQN5dsRBJApIok0jFn-9CS842lGpLmqGAYiOoDRAgmlkgnY0gmlwhLhIgb2Hb3BzdGFja4OFQgCJc2VjcDI1NmsxoQJ9FTIv8B9myn1MWaC_2lJ-sMoeCDkusCsk4BYHjjCq04N0Y3CCJAaDdWRwgiQG",
	},

	34443: {
		"enr:-J24QNz9lbrKbN4iSmmjtnr7SjUMk4zB7f1krHZcTZx-JRKZd0kA2gjufUROD6T3sOWDVDnFJRvqBBo62zuF-hYCohOGAYiOoEyEgmlkgnY0gmlwhAPniryHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQKNVFlCxh_B-716tTs-h1vMzZkSs1FTu_OYTNjgufplG4N0Y3CCJAaDdWRwgiQG",
	},

	252: {
		"enr:-J24QNz9lbrKbN4iSmmjtnr7SjUMk4zB7f1krHZcTZx-JRKZd0kA2gjufUROD6T3sOWDVDnFJRvqBBo62zuF-hYCohOGAYiOoEyEgmlkgnY0gmlwhAPniryHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQKNVFlCxh_B-716tTs-h1vMzZkSs1FTu_OYTNjgufplG4N0Y3CCJAaDdWRwgiQG",
	},

	480: {
		"enr:-J24QNz9lbrKbN4iSmmjtnr7SjUMk4zB7f1krHZcTZx-JRKZd0kA2gjufUROD6T3sOWDVDnFJRvqBBo62zuF-hYCohOGAYiOoEyEgmlkgnY0gmlwhAPniryHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQKNVFlCxh_B-716tTs-h1vMzZkSs1FTu_OYTNjgufplG4N0Y3CCJAaDdWRwgiQG",
	},

	7560: {
		"enr:-J24QNz9lbrKbN4iSmmjtnr7SjUMk4zB7f1krHZcTZx-JRKZd0kA2gjufUROD6T3sOWDVDnFJRvqBBo62zuF-hYCohOGAYiOoEyEgmlkgnY0gmlwhAPniryHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQKNVFlCxh_B-716tTs-h1vMzZkSs1FTu_OYTNjgufplG4N0Y3CCJAaDdWRwgiQG",
	},

	185: {
		"enr:-J24QNz9lbrKbN4iSmmjtnr7SjUMk4zB7f1krHZcTZx-JRKZd0kA2gjufUROD6T3sOWDVDnFJRvqBBo62zuF-hYCohOGAYiOoEyEgmlkgnY0gmlwhAPniryHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQKNVFlCxh_B-716tTs-h1vMzZkSs1FTu_OYTNjgufplG4N0Y3CCJAaDdWRwgiQG",
	},

	690: {
		"enr:-J24QNz9lbrKbN4iSmmjtnr7SjUMk4zB7f1krHZcTZx-JRKZd0kA2gjufUROD6T3sOWDVDnFJRvqBBo62zuF-hYCohOGAYiOoEyEgmlkgnY0gmlwhAPniryHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQKNVFlCxh_B-716tTs-h1vMzZkSs1FTu_OYTNjgufplG4N0Y3CCJAaDdWRwgiQG",
	},

	1135: {
		"enr:-J24QNz9lbrKbN4iSmmjtnr7SjUMk4zB7f1krHZcTZx-JRKZd0kA2gjufUROD6T3sOWDVDnFJRvqBBo62zuF-hYCohOGAYiOoEyEgmlkgnY0gmlwhAPniryHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQKNVFlCxh_B-716tTs-h1vMzZkSs1FTu_OYTNjgufplG4N0Y3CCJAaDdWRwgiQG",
	},

	84532: {
		"enr:-J24QNz9lbrKbN4iSmmjtnr7SjUMk4zB7f1krHZcTZx-JRKZd0kA2gjufUROD6T3sOWDVDnFJRvqBBo62zuF-hYCohOGAYiOoEyEgmlkgnY0gmlwhAPniryHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQKNVFlCxh_B-716tTs-h1vMzZkSs1FTu_OYTNjgufplG4N0Y3CCJAaDdWRwgiQG",
	},

	11155420: {
		"enr:-J24QNz9lbrKbN4iSmmjtnr7SjUMk4zB7f1krHZcTZx-JRKZd0kA2gjufUROD6T3sOWDVDnFJRvqBBo62zuF-hYCohOGAYiOoEyEgmlkgnY0gmlwhAPniryHb3BzdGFja4OFQgCJc2VjcDI1NmsxoQKNVFlCxh_B-716tTs-h1vMzZkSs1FTu_OYTNjgufplG4N0Y3CCJAaDdWRwgiQG",
	},
}

func NewOpNodeGossip(privateKey *ecdsa.PrivateKey) (*OpNodeGossip, error) {
	ctx, cancel := context.WithCancel(context.Background())

	privKeyBytes := crypto.FromECDSA(privateKey)
	libp2pPrivKey, err := libp2pcrypto.UnmarshalSecp256k1PrivateKey(privKeyBytes)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to convert key: %w", err)
	}

	h, err := libp2p.New(
		libp2p.Identity(libp2pPrivKey),
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
			"/ip6/::/tcp/0",
		),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
		libp2p.EnableNATService(),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	return newOpNodeGossipWithHost(h, true, privateKey, ctx, cancel)
}

func NewOpNodeGossipWithSharedHost(sharedHost host.Host, privateKey *ecdsa.PrivateKey) (*OpNodeGossip, error) {
	ctx, cancel := context.WithCancel(context.Background())
	return newOpNodeGossipWithHost(sharedHost, false, privateKey, ctx, cancel)
}

func newOpNodeGossipWithHost(h host.Host, owned bool, privateKey *ecdsa.PrivateKey, ctx context.Context, cancel context.CancelFunc) (*OpNodeGossip, error) {

	ps, err := pubsub.NewGossipSub(ctx, h,
		pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign),
		pubsub.WithFloodPublish(true),
		pubsub.WithPeerExchange(true),
	)
	if err != nil {
		if owned {
			h.Close()
		}
		cancel()
		return nil, fmt.Errorf("failed to create gossipsub: %w", err)
	}

	return &OpNodeGossip{
		host:           h,
		hostOwned:      owned,
		pubsub:         ps,
		privateKey:     privateKey,
		topics:         make(map[uint64]*pubsub.Topic),
		subs:           make(map[uint64]*pubsub.Subscription),
		blocksReceived: make(map[uint64]uint64),
		ctx:            ctx,
		cancel:         cancel,
	}, nil
}

func (g *OpNodeGossip) Start() error {
	fmt.Printf("[OpNodeGossip] Started with peer ID: %s\n", g.host.ID().String()[:16])
	fmt.Printf("[OpNodeGossip] Listening on: %v\n", g.host.Addrs())

	go g.bootstrapConnections()

	return nil
}

func (g *OpNodeGossip) bootstrapConnections() {

	time.Sleep(2 * time.Second)

	connectedPeers := 0
	failedPeers := 0
	seenPeers := make(map[peer.ID]bool)

	fmt.Printf("[OpNodeGossip] Starting L2 bootstrap with %d chain configs...\n", len(opStackBootnodes))

	for chainID, enrs := range opStackBootnodes {
		for _, enrStr := range enrs {

			peerInfo, err := ENRToAddrInfo(enrStr)
			if err != nil {
				fmt.Printf("[OpNodeGossip] Chain %d: ENR parse error: %v\n", chainID, err)
				continue
			}

			if seenPeers[peerInfo.ID] {
				continue
			}
			seenPeers[peerInfo.ID] = true

			addrs := peerInfo.Addrs
			addrStr := "no-addr"
			if len(addrs) > 0 {
				addrStr = addrs[0].String()
			}

			ctx, cancel := context.WithTimeout(g.ctx, 15*time.Second)
			if err := g.host.Connect(ctx, *peerInfo); err != nil {
				failedPeers++

				if failedPeers <= 5 {
					fmt.Printf("[OpNodeGossip] Chain %d: connect failed to %s (%s): %v\n",
						chainID, peerInfo.ID.String()[:12], addrStr, err)
				}
			} else {
				connectedPeers++
				fmt.Printf("[OpNodeGossip] Chain %d: connected to %s (%s)\n",
					chainID, peerInfo.ID.String()[:12], addrStr)
			}
			cancel()
		}
	}

	fmt.Printf("[OpNodeGossip] Bootstrap complete: %d connected, %d failed\n", connectedPeers, failedPeers)

	peers := g.host.Network().Peers()
	fmt.Printf("[OpNodeGossip] Total host peers: %d\n", len(peers))
}

func (g *OpNodeGossip) Publish(chainID uint64, data []byte) error {
	g.mu.RLock()
	topic, ok := g.topics[chainID]
	g.mu.RUnlock()

	if !ok {
		return fmt.Errorf("chain %d not subscribed", chainID)
	}

	return topic.Publish(g.ctx, data)
}

func (g *OpNodeGossip) PublishToAll(data []byte) map[uint64]error {
	g.mu.RLock()
	chains := make([]uint64, 0, len(g.topics))
	for chainID := range g.topics {
		chains = append(chains, chainID)
	}
	g.mu.RUnlock()

	results := make(map[uint64]error)
	for _, chainID := range chains {
		results[chainID] = g.Publish(chainID, data)
	}
	return results
}

func (g *OpNodeGossip) SubscribeChain(chainID uint64) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, exists := g.topics[chainID]; exists {
		return nil
	}

	versions := []int{2, 3, 4, 5}

	var firstTopic *pubsub.Topic
	var firstSub *pubsub.Subscription
	var subscribedVersions []int

	for _, version := range versions {
		topicName := fmt.Sprintf("/optimism/%d/%d/blocks", chainID, version)

		topic, err := g.pubsub.Join(topicName)
		if err != nil {
			fmt.Printf("[OpNodeGossip] Failed to join %s: %v\n", topicName, err)
			continue
		}

		sub, err := topic.Subscribe()
		if err != nil {
			topic.Close()
			fmt.Printf("[OpNodeGossip] Failed to subscribe to %s: %v\n", topicName, err)
			continue
		}

		subscribedVersions = append(subscribedVersions, version)

		go g.handleMessages(chainID, sub)

		if firstTopic == nil {
			firstTopic = topic
			firstSub = sub
		}
	}

	if firstTopic == nil {
		return fmt.Errorf("failed to subscribe to any topic version for chain %d", chainID)
	}

	g.topics[chainID] = firstTopic
	g.subs[chainID] = firstSub

	fmt.Printf("[OpNodeGossip] Chain %d: subscribed to versions %v\n", chainID, subscribedVersions)
	return nil
}

func (g *OpNodeGossip) handleMessages(chainID uint64, sub *pubsub.Subscription) {
	for {
		msg, err := sub.Next(g.ctx)
		if err != nil {
			if g.ctx.Err() != nil {
				return
			}
			fmt.Printf("[OpNodeGossip] Error receiving message on chain %d: %v\n", chainID, err)
			continue
		}

		if msg.ReceivedFrom == g.host.ID() {
			continue
		}

		block, err := g.decodeBlockGossip(chainID, msg.Data, msg.ReceivedFrom.String())
		if err != nil {

			g.statsMu.Lock()
			g.decodeErrors++
			if g.decodeErrors%100 == 1 {
				fmt.Printf("[L2] Chain %d decode error (count=%d): %v\n", chainID, g.decodeErrors, err)
			}
			g.statsMu.Unlock()
			continue
		}

		g.statsMu.Lock()
		g.blocksReceived[chainID]++
		count := g.blocksReceived[chainID]
		g.statsMu.Unlock()

		if count%10 == 1 {
			fmt.Printf("[L2] Chain %d block #%d (%d txs)\n", chainID, block.BlockNumber, len(block.Transactions))
		}

		if g.OnBlockReceived != nil {
			g.OnBlockReceived(chainID, block)
		}
	}
}

func (g *OpNodeGossip) decodeBlockGossip(chainID uint64, data []byte, fromPeer string) (*L2BlockGossip, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("message too short: %d bytes", len(data))
	}

	decompressed, err := snappy.Decode(nil, data)
	if err != nil {

		decompressed = data
	}

	var payload ExecutionPayloadEnvelope
	if err := rlp.DecodeBytes(decompressed, &payload); err == nil {
		return &L2BlockGossip{
			ChainID:      chainID,
			BlockNumber:  payload.BlockNumber,
			BlockHash:    payload.BlockHash,
			ParentHash:   payload.ParentHash,
			Timestamp:    payload.Timestamp,
			Transactions: payload.Transactions,
			ReceivedAt:   time.Now(),
			FromPeer:     fromPeer,
		}, nil
	}

	if len(decompressed) >= 100 {
		block := &L2BlockGossip{
			ChainID:    chainID,
			ReceivedAt: time.Now(),
			FromPeer:   fromPeer,
		}

		copy(block.ParentHash[:], decompressed[0:32])

		if len(decompressed) >= 60 {
			block.BlockNumber = binary.LittleEndian.Uint64(decompressed[52:60])
		}
		if len(decompressed) >= 84 {
			block.Timestamp = binary.LittleEndian.Uint64(decompressed[76:84])
		}

		if block.BlockNumber > 0 {
			return block, nil
		}
	}

	return nil, fmt.Errorf("failed to decode payload (tried RLP and SSZ): len=%d", len(decompressed))
}

func (g *OpNodeGossip) connectToBootnodes(chainID uint64) {

	bootnodes := getBootnodeMultiaddrs()
	if len(bootnodes) == 0 {
		fmt.Printf("[OpNodeGossip] No bootnodes available for chain %d\n", chainID)
		return
	}

	var connected int
	for _, maddr := range bootnodes {

		peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			fmt.Printf("[OpNodeGossip] Invalid bootnode multiaddr: %s\n", maddr.String())
			continue
		}

		ctx, cancel := context.WithTimeout(g.ctx, 10*time.Second)
		if err := g.host.Connect(ctx, *peerInfo); err != nil {

			peerIDStr := peerInfo.ID.String()
			if len(peerIDStr) > 8 {
				peerIDStr = peerIDStr[:8]
			}
			fmt.Printf("[OpNodeGossip] Failed to connect to bootnode %s: %v\n", peerIDStr, err)
		} else {
			peerIDStr := peerInfo.ID.String()
			if len(peerIDStr) > 8 {
				peerIDStr = peerIDStr[:8]
			}
			fmt.Printf("[OpNodeGossip] Connected to bootnode %s for chain %d\n", peerIDStr, chainID)
			connected++
		}
		cancel()
	}

	fmt.Printf("[OpNodeGossip] Connected to %d/%d bootnodes for chain %d\n", connected, len(bootnodes), chainID)
}

func (g *OpNodeGossip) GetPeerCount(chainID uint64) int {
	g.mu.RLock()
	topic, ok := g.topics[chainID]
	g.mu.RUnlock()

	if !ok {
		return 0
	}

	return len(topic.ListPeers())
}

func (g *OpNodeGossip) GetStats() map[uint64]map[string]interface{} {
	g.statsMu.RLock()
	defer g.statsMu.RUnlock()

	stats := make(map[uint64]map[string]interface{})
	for chainID := range g.topics {
		stats[chainID] = map[string]interface{}{
			"blocks_received": g.blocksReceived[chainID],
			"peer_count":      g.GetPeerCount(chainID),
			"topic":           fmt.Sprintf("/optimism/%d/0/blocks", chainID),
		}
	}
	return stats
}

func (g *OpNodeGossip) PeerID() string {
	return g.host.ID().String()
}

func (g *OpNodeGossip) Stop() {
	g.cancel()

	g.mu.Lock()
	for _, sub := range g.subs {
		sub.Cancel()
	}
	for _, topic := range g.topics {
		topic.Close()
	}
	g.mu.Unlock()

	if g.hostOwned {
		g.host.Close()
	}
	fmt.Printf("[OpNodeGossip] Stopped\n")
}

func (block *L2BlockGossip) ExtractTransactionHashes() []common.Hash {
	hashes := make([]common.Hash, 0, len(block.Transactions))
	for _, txBytes := range block.Transactions {

		hash := crypto.Keccak256Hash(txBytes)
		hashes = append(hashes, hash)
	}
	return hashes
}

func (block *L2BlockGossip) ExtractFullTransactions() []*types.Transaction {
	txs := make([]*types.Transaction, 0, len(block.Transactions))
	for _, txBytes := range block.Transactions {
		var tx types.Transaction
		if err := rlp.DecodeBytes(txBytes, &tx); err != nil {

			continue
		}
		txs = append(txs, &tx)
	}
	return txs
}

func encodeChainID(chainID uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, chainID)
	return buf
}

func enodeToMultiaddr(enodeURL string) (string, error) {

	pattern := regexp.MustCompile(`^enode://([0-9a-fA-F]{128})@([^:]+):(\d+)$`)
	matches := pattern.FindStringSubmatch(enodeURL)
	if len(matches) != 4 {
		return "", fmt.Errorf("invalid enode format: %s", enodeURL)
	}

	pubkeyHex := matches[1]
	host := matches[2]
	port := matches[3]

	pubkeyBytes, err := hex.DecodeString(pubkeyHex)
	if err != nil {
		return "", fmt.Errorf("failed to decode pubkey hex: %w", err)
	}

	fullPubkey := append([]byte{0x04}, pubkeyBytes...)

	libp2pPubKey, err := libp2pcrypto.UnmarshalSecp256k1PublicKey(fullPubkey)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal pubkey: %w", err)
	}

	peerID, err := peer.IDFromPublicKey(libp2pPubKey)
	if err != nil {
		return "", fmt.Errorf("failed to derive peer ID: %w", err)
	}

	isIPv4 := regexp.MustCompile(`^[\d\.]+$`).MatchString(host)

	var multiaddr string
	if isIPv4 {
		multiaddr = fmt.Sprintf("/ip4/%s/tcp/%s/p2p/%s", host, port, peerID.String())
	} else {

		multiaddr = fmt.Sprintf("/dns4/%s/tcp/%s/p2p/%s", host, port, peerID.String())
	}

	return multiaddr, nil
}

func getBootnodeMultiaddrs() []ma.Multiaddr {
	var addrs []ma.Multiaddr
	seen := make(map[string]bool)

	for provider, enodes := range opStackBootnodeENRs {
		for _, enode := range enodes {
			maddr, err := enodeToMultiaddr(enode)
			if err != nil {
				fmt.Printf("[OpNodeGossip] Failed to convert enode from %s: %v\n", provider, err)
				continue
			}

			if seen[maddr] {
				continue
			}
			seen[maddr] = true

			parsed, err := ma.NewMultiaddr(maddr)
			if err != nil {
				fmt.Printf("[OpNodeGossip] Failed to parse multiaddr: %v\n", err)
				continue
			}
			addrs = append(addrs, parsed)
		}
	}

	return addrs
}

func (g *OpNodeGossip) JoinCustomTopic(topicName string) (*pubsub.Topic, *pubsub.Subscription, error) {
	topic, err := g.pubsub.Join(topicName)
	if err != nil {
		return nil, nil, err
	}
	sub, err := topic.Subscribe()
	if err != nil {
		topic.Close()
		return nil, nil, err
	}
	return topic, sub, nil
}

const PriceGossipTopic = "/zeam/prices/1.0.0"

func (g *OpNodeGossip) SubscribePriceGossip() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.priceTopic != nil {
		return nil
	}

	topic, err := g.pubsub.Join(PriceGossipTopic)
	if err != nil {
		return fmt.Errorf("failed to join price topic: %w", err)
	}

	sub, err := topic.Subscribe()
	if err != nil {
		topic.Close()
		return fmt.Errorf("failed to subscribe to price topic: %w", err)
	}

	g.priceTopic = topic
	g.priceSub = sub

	go g.handlePriceMessages(sub)

	fmt.Printf("[OpNodeGossip] Subscribed to price gossip: %s\n", PriceGossipTopic)
	return nil
}

func (g *OpNodeGossip) handlePriceMessages(sub *pubsub.Subscription) {
	for {
		msg, err := sub.Next(g.ctx)
		if err != nil {
			if g.ctx.Err() != nil {
				return
			}
			continue
		}

		if msg.ReceivedFrom == g.host.ID() {
			continue
		}

		g.statsMu.Lock()
		g.pricesReceived++
		g.statsMu.Unlock()

		if g.OnPriceGossip != nil {
			g.OnPriceGossip(msg.Data)
		}
	}
}

func (g *OpNodeGossip) PublishPriceGossip(data []byte) error {
	g.mu.RLock()
	topic := g.priceTopic
	g.mu.RUnlock()

	if topic == nil {
		return fmt.Errorf("price gossip not subscribed")
	}

	return topic.Publish(g.ctx, data)
}

func (g *OpNodeGossip) GetPriceGossipPeerCount() int {
	g.mu.RLock()
	topic := g.priceTopic
	g.mu.RUnlock()

	if topic == nil {
		return 0
	}
	return len(topic.ListPeers())
}
