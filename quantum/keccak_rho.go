package quantum

import (
	"encoding/binary"
	"math/big"

	"github.com/ethereum/go-ethereum/crypto"
)

func KeccakStep(n, x *big.Int) *big.Int {
	data := make([]byte, 64)
	nBytes := n.Bytes()
	xBytes := x.Bytes()
	copy(data[32-len(nBytes):32], nBytes)
	copy(data[64-len(xBytes):64], xBytes)

	hash := crypto.Keccak256(data)
	result := new(big.Int).SetBytes(hash)
	return result.Mod(result, n)
}

func IsKeccakDP(x *big.Int, bits int) bool {
	if bits == 0 {
		return true
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	return new(big.Int).And(x, mask).Sign() == 0
}

const MsgTypeKeccakRho byte = 0x4B

func EncodeKeccakRhoPayload(n, x *big.Int, walkID uint32) []byte {
	payload := make([]byte, 72)
	payload[0] = 0x5A
	payload[1] = 0x45
	payload[2] = 0x01
	payload[3] = MsgTypeKeccakRho

	binary.BigEndian.PutUint32(payload[4:8], walkID)

	nBytes := n.Bytes()
	copy(payload[40-len(nBytes):40], nBytes)

	xBytes := x.Bytes()
	copy(payload[72-len(xBytes):72], xBytes)

	return payload
}

func DecodeKeccakRhoPayload(data []byte) (walkID uint32, n, x *big.Int, ok bool) {
	if len(data) < 72 {
		return 0, nil, nil, false
	}
	if data[0] != 0x5A || data[1] != 0x45 || data[3] != MsgTypeKeccakRho {
		return 0, nil, nil, false
	}

	walkID = binary.BigEndian.Uint32(data[4:8])
	n = new(big.Int).SetBytes(data[8:40])
	x = new(big.Int).SetBytes(data[40:72])
	return walkID, n, x, true
}
