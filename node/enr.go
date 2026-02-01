package node

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	ma "github.com/multiformats/go-multiaddr"
)

func ENRToMultiaddr(enrStr string) (ma.Multiaddr, peer.ID, error) {

	node, err := enode.Parse(enode.ValidSchemes, enrStr)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse ENR: %w", err)
	}

	ip := node.IP()
	if ip == nil {
		return nil, "", fmt.Errorf("ENR has no IP address")
	}

	tcpPort := node.TCP()
	if tcpPort == 0 {

		tcpPort = node.UDP()
		if tcpPort == 0 {
			return nil, "", fmt.Errorf("ENR has no TCP or UDP port")
		}
	}

	pubkey := node.Pubkey()
	if pubkey == nil {
		return nil, "", fmt.Errorf("ENR has no public key")
	}

	pubkeyBytes := crypto.FromECDSAPub(pubkey)
	libp2pPubkey, err := libp2pcrypto.UnmarshalSecp256k1PublicKey(pubkeyBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to convert pubkey: %w", err)
	}

	peerID, err := peer.IDFromPublicKey(libp2pPubkey)
	if err != nil {
		return nil, "", fmt.Errorf("failed to derive peer ID: %w", err)
	}

	var maStr string
	if ip4 := ip.To4(); ip4 != nil {
		maStr = fmt.Sprintf("/ip4/%s/tcp/%d/p2p/%s", ip4.String(), tcpPort, peerID.String())
	} else {
		maStr = fmt.Sprintf("/ip6/%s/tcp/%d/p2p/%s", ip.String(), tcpPort, peerID.String())
	}

	maddr, err := ma.NewMultiaddr(maStr)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create multiaddr: %w", err)
	}

	return maddr, peerID, nil
}

func ENRToAddrInfo(enrStr string) (*peer.AddrInfo, error) {
	maddr, peerID, err := ENRToMultiaddr(enrStr)
	if err != nil {
		return nil, err
	}

	transportAddr, _ := ma.SplitFunc(maddr, func(c ma.Component) bool {
		return c.Protocol().Code == ma.P_P2P
	})

	return &peer.AddrInfo{
		ID:    peerID,
		Addrs: []ma.Multiaddr{transportAddr},
	}, nil
}

func ParseENRList(enrList string) ([]*peer.AddrInfo, error) {
	if enrList == "" {
		return nil, nil
	}

	var results []*peer.AddrInfo
	for _, enr := range strings.Split(enrList, ",") {
		enr = strings.TrimSpace(enr)
		if enr == "" {
			continue
		}

		info, err := ENRToAddrInfo(enr)
		if err != nil {

			fmt.Printf("[ENR] Failed to parse: %s: %v\n", enr[:min(30, len(enr))], err)
			continue
		}
		results = append(results, info)
	}

	return results, nil
}

func DecodeENRRaw(enrStr string) (ip net.IP, tcpPort uint16, pubkey []byte, err error) {

	if strings.HasPrefix(enrStr, "enr:") {
		enrStr = enrStr[4:]
	}

	data, err := base64.RawURLEncoding.DecodeString(enrStr)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("base64 decode failed: %w", err)
	}

	var content []interface{}
	if err := rlp.Decode(bytes.NewReader(data), &content); err != nil {
		return nil, 0, nil, fmt.Errorf("RLP decode failed: %w", err)
	}

	if len(content) < 4 {
		return nil, 0, nil, fmt.Errorf("ENR too short")
	}

	for i := 2; i < len(content)-1; i += 2 {
		key, ok := content[i].([]byte)
		if !ok {
			continue
		}

		keyStr := string(key)
		value := content[i+1]

		switch keyStr {
		case "ip":
			if ipBytes, ok := value.([]byte); ok {
				ip = net.IP(ipBytes)
			}
		case "ip6":
			if ipBytes, ok := value.([]byte); ok && ip == nil {
				ip = net.IP(ipBytes)
			}
		case "tcp":
			if portBytes, ok := value.([]byte); ok && len(portBytes) <= 2 {
				for _, b := range portBytes {
					tcpPort = tcpPort<<8 | uint16(b)
				}
			}
		case "secp256k1":
			if pkBytes, ok := value.([]byte); ok {
				pubkey = pkBytes
			}
		}
	}

	if ip == nil {
		return nil, 0, nil, fmt.Errorf("no IP found in ENR")
	}
	if tcpPort == 0 {
		return nil, 0, nil, fmt.Errorf("no TCP port found in ENR")
	}
	if pubkey == nil {
		return nil, 0, nil, fmt.Errorf("no pubkey found in ENR")
	}

	return ip, tcpPort, pubkey, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
