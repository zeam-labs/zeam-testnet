

package node

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	ma "github.com/multiformats/go-multiaddr"
)

const (
	
	ProxyProtocolID = "/zeam/tcp-proxy/1.0.0"
)


type LibP2PTransport struct {
	host       host.Host
	dht        *dht.IpfsDHT
	privateKey *ecdsa.PrivateKey

	
	relayAddrs   []ma.Multiaddr
	relayAddrsMu sync.RWMutex

	
	proxies   map[string]*proxyConn
	proxiesMu sync.RWMutex

	
	ctx    context.Context
	cancel context.CancelFunc
}


type proxyConn struct {
	stream network.Stream
	conn   net.Conn
}


var ipfsBootstrapNodes = []string{
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",
	"/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ",
}


func NewLibP2PTransport(privateKey *ecdsa.PrivateKey) (*LibP2PTransport, error) {
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

	
	kadDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeClient))
	if err != nil {
		h.Close()
		cancel()
		return nil, fmt.Errorf("failed to create DHT: %w", err)
	}

	t := &LibP2PTransport{
		host:       h,
		dht:        kadDHT,
		privateKey: privateKey,
		proxies:    make(map[string]*proxyConn),
		ctx:        ctx,
		cancel:     cancel,
	}

	return t, nil
}


func (t *LibP2PTransport) Start() error {
	fmt.Printf("[LibP2P] Starting with peer ID: %s\n", t.host.ID().String()[:16])

	
	if err := t.bootstrap(); err != nil {
		return fmt.Errorf("failed to bootstrap: %w", err)
	}

	
	if err := t.dht.Bootstrap(t.ctx); err != nil {
		return fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	
	go t.discoverRelays()

	fmt.Printf("[LibP2P] Connected to IPFS network\n")
	fmt.Printf("[LibP2P] Local addrs: %v\n", t.host.Addrs())

	return nil
}


func (t *LibP2PTransport) bootstrap() error {
	var wg sync.WaitGroup
	var successCount int
	var mu sync.Mutex

	for _, addrStr := range ipfsBootstrapNodes {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()

			maddr, err := ma.NewMultiaddr(addr)
			if err != nil {
				return
			}

			peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				return
			}

			ctx, cancel := context.WithTimeout(t.ctx, 10*time.Second)
			defer cancel()

			if err := t.host.Connect(ctx, *peerInfo); err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(addrStr)
	}

	wg.Wait()

	if successCount == 0 {
		return fmt.Errorf("failed to connect to any bootstrap nodes")
	}

	fmt.Printf("[LibP2P] Connected to %d bootstrap nodes\n", successCount)
	return nil
}


func (t *LibP2PTransport) discoverRelays() {
	
	time.Sleep(5 * time.Second)

	
	for _, p := range t.host.Network().Peers() {
		
		go func(peerID peer.ID) {
			ctx, cancel := context.WithTimeout(t.ctx, 30*time.Second)
			defer cancel()

			
			reservation, err := client.Reserve(ctx, t.host, peer.AddrInfo{ID: peerID})
			if err != nil {
				return 
			}

			
			t.relayAddrsMu.Lock()
			for _, addr := range reservation.Addrs {
				relayAddr := addr.Encapsulate(ma.StringCast("/p2p/" + t.host.ID().String()))
				t.relayAddrs = append(t.relayAddrs, relayAddr)
				fmt.Printf("[LibP2P] Got relay address: %s\n", relayAddr.String()[:60]+"...")
			}
			t.relayAddrsMu.Unlock()
		}(p)
	}
}


func (t *LibP2PTransport) DialTCP(address string) (net.Conn, error) {
	
	
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}

	
	targetMA, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%s", host, port))
	if err != nil {
		return nil, fmt.Errorf("failed to create multiaddr: %w", err)
	}

	
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	conn, err := dialer.DialContext(t.ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w (multiaddr: %s)", address, err, targetMA)
	}

	return conn, nil
}


func (t *LibP2PTransport) GetRelayAddrs() []string {
	t.relayAddrsMu.RLock()
	defer t.relayAddrsMu.RUnlock()

	addrs := make([]string, len(t.relayAddrs))
	for i, addr := range t.relayAddrs {
		addrs[i] = addr.String()
	}
	return addrs
}


func (t *LibP2PTransport) PeerID() string {
	return t.host.ID().String()
}


func (t *LibP2PTransport) ConnectedPeers() int {
	return len(t.host.Network().Peers())
}


func (t *LibP2PTransport) Stop() {
	t.cancel()
	t.dht.Close()
	t.host.Close()
	fmt.Printf("[LibP2P] Stopped\n")
}


var _ io.ReadWriteCloser = (*proxyConn)(nil)

func (p *proxyConn) Read(b []byte) (n int, err error) {
	return p.stream.Read(b)
}

func (p *proxyConn) Write(b []byte) (n int, err error) {
	return p.stream.Write(b)
}

func (p *proxyConn) Close() error {
	p.stream.Close()
	if p.conn != nil {
		p.conn.Close()
	}
	return nil
}
