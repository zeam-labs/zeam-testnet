package quantum

import (
	"crypto/sha256"
	"math/big"
)

type SubstrateChain struct {
	*Chain
	State SubstrateState
}

type SubstrateState struct {
	Amplitudes   map[string]complex128
	Phases       map[string]complex128
	Entanglements map[string][]string
	Genesis      [32]byte
}

func NewSubstrateChain(chain *Chain) *SubstrateChain {

	genesis := sha256.Sum256([]byte(chain.ID))

	return &SubstrateChain{
		Chain: chain,
		State: SubstrateState{
			Amplitudes:    make(map[string]complex128),
			Phases:        make(map[string]complex128),
			Entanglements: make(map[string][]string),
			Genesis:       genesis,
		},
	}
}

func FeistelHash(x *big.Int) *big.Int {
	if x == nil {
		return big.NewInt(0)
	}

	bytes := x.Bytes()
	if len(bytes) == 0 {
		bytes = []byte{0}
	}

	if len(bytes)%2 != 0 {
		bytes = append([]byte{0}, bytes...)
	}

	mid := len(bytes) / 2
	left := bytes[:mid]
	right := bytes[mid:]

	newRight := make([]byte, mid)
	hashBuf := make([]byte, 0, 32)
	roundSalt := [1]byte{}

	h := sha256.New()

	for round := 0; round < 4; round++ {

		h.Reset()
		h.Write(right)
		roundSalt[0] = byte(round)
		h.Write(roundSalt[:])
		hashBuf = h.Sum(hashBuf[:0])

		hashLen := len(left)
		if hashLen > 32 {
			hashLen = 32
		}

		chunks := hashLen / 8
		for i := 0; i < chunks; i++ {
			offset := i * 8
			l := uint64(left[offset]) | uint64(left[offset+1])<<8 |
				uint64(left[offset+2])<<16 | uint64(left[offset+3])<<24 |
				uint64(left[offset+4])<<32 | uint64(left[offset+5])<<40 |
				uint64(left[offset+6])<<48 | uint64(left[offset+7])<<56
			hv := uint64(hashBuf[offset]) | uint64(hashBuf[offset+1])<<8 |
				uint64(hashBuf[offset+2])<<16 | uint64(hashBuf[offset+3])<<24 |
				uint64(hashBuf[offset+4])<<32 | uint64(hashBuf[offset+5])<<40 |
				uint64(hashBuf[offset+6])<<48 | uint64(hashBuf[offset+7])<<56
			x := l ^ hv
			newRight[offset] = byte(x)
			newRight[offset+1] = byte(x >> 8)
			newRight[offset+2] = byte(x >> 16)
			newRight[offset+3] = byte(x >> 24)
			newRight[offset+4] = byte(x >> 32)
			newRight[offset+5] = byte(x >> 40)
			newRight[offset+6] = byte(x >> 48)
			newRight[offset+7] = byte(x >> 56)
		}

		for i := chunks * 8; i < len(left); i++ {
			if i < hashLen {
				newRight[i] = left[i] ^ hashBuf[i]
			} else {
				newRight[i] = left[i]
			}
		}

		left, right, newRight = right, newRight, left
	}

	result := make([]byte, len(left)+len(right))
	copy(result, left)
	copy(result[len(left):], right)
	return new(big.Int).SetBytes(result)
}

func UTF8_ENCODE(sc *SubstrateChain, text string) *big.Int {
	if sc == nil || text == "" {
		return big.NewInt(0)
	}

	coord := new(big.Int).SetBytes(sc.State.Genesis[:8])

	for _, ch := range text {

		charVal := big.NewInt(int64(ch))

		coord.Lsh(coord, 8)
		coord.Add(coord, charVal)

		coord = FeistelHash(coord)
	}

	return coord
}

func UTF8_DECODE(coord *big.Int) string {
	if coord == nil || coord.Sign() == 0 {
		return ""
	}

	bytes := coord.Bytes()
	if len(bytes) == 0 {
		return ""
	}

	var result []byte
	for _, b := range bytes {
		if b >= 32 && b <= 126 {
			result = append(result, b)
		}
	}

	if len(result) > 64 {
		result = result[:64]
	}

	return string(result)
}

func COMPOSE(sc *SubstrateChain, a, b *big.Int) {
	if sc == nil || a == nil || b == nil {
		return
	}

	keyA := a.String()
	keyB := b.String()

	sc.State.Entanglements[keyA] = append(sc.State.Entanglements[keyA], keyB)
	sc.State.Entanglements[keyB] = append(sc.State.Entanglements[keyB], keyA)
}

type QuantumCircuit func(sc *SubstrateChain, inputs ...*big.Int) *big.Int

func (sc *SubstrateChain) GetEntanglements(coord *big.Int) []*big.Int {
	if coord == nil {
		return nil
	}

	key := coord.String()
	entangled := sc.State.Entanglements[key]

	result := make([]*big.Int, len(entangled))
	for i, e := range entangled {
		result[i], _ = new(big.Int).SetString(e, 10)
	}

	return result
}

func (sc *SubstrateChain) SetAmplitude(coord *big.Int, amp complex128) {
	if coord != nil {
		sc.State.Amplitudes[coord.String()] = amp
	}
}

func (sc *SubstrateChain) GetAmplitude(coord *big.Int) complex128 {
	if coord == nil {
		return 0
	}
	return sc.State.Amplitudes[coord.String()]
}

func (sc *SubstrateChain) SetPhase(coord *big.Int, phase complex128) {
	if coord != nil {
		sc.State.Phases[coord.String()] = phase
	}
}

func (sc *SubstrateChain) GetPhase(coord *big.Int) complex128 {
	if coord == nil {
		return 0
	}
	return sc.State.Phases[coord.String()]
}
