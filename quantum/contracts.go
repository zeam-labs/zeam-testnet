package quantum

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type QuantumContract struct {
	Contexts  []string
	InitState string
	Transform string
	Branch    string
	T         int
	K         int
	Phase     string
}

type ExecutionResult struct {
	Result   *big.Int
	Pressure PressureMetrics
}

type PressureMetrics struct {
	Hadamard float64
	PauliX   float64
	PauliZ   float64
	Phase    float64
}

type ContractRegistry struct {
	contracts map[string]QuantumContract
	counter   uint64
	mu        sync.RWMutex
}

var globalRegistry = &ContractRegistry{
	contracts: make(map[string]QuantumContract),
}

func DeployContract(contract QuantumContract) string {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	id := fmt.Sprintf("contract_%d", atomic.AddUint64(&globalRegistry.counter, 1))
	globalRegistry.contracts[id] = contract
	return id
}

func ExecuteContract(sc *SubstrateChain, contractID string) (*ExecutionResult, error) {
	globalRegistry.mu.RLock()
	contract, exists := globalRegistry.contracts[contractID]
	globalRegistry.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("contract %s not found", contractID)
	}

	return executeContract(sc, contract)
}

func executeContract(sc *SubstrateChain, contract QuantumContract) (*ExecutionResult, error) {

	initVal := big.NewInt(0)
	if contract.InitState != "" {
		if v, ok := new(big.Int).SetString(contract.InitState, 10); ok {
			initVal = v
		}
	}

	state := new(big.Int).Set(initVal)
	for _, ctx := range contract.Contexts {
		state = applyContext(sc, state, ctx)
	}

	for i := 0; i < contract.T; i++ {
		switch contract.Transform {
		case "affine":
			state = affineTransform(state, contract.K, i)
		case "feistel":
			state = FeistelHash(state)
		case "compose":

			shifted := new(big.Int).Lsh(state, uint(contract.K%64))
			state.Xor(state, shifted)
		default:
			state = FeistelHash(state)
		}

		if contract.Phase == "alt" && i%2 == 1 {
			state.Neg(state)
			state.Abs(state)
		}
	}

	pressure := computePressure(sc, state)

	return &ExecutionResult{
		Result:   state,
		Pressure: pressure,
	}, nil
}

func applyContext(sc *SubstrateChain, state *big.Int, ctx string) *big.Int {
	parts := strings.SplitN(ctx, ":", 2)
	if len(parts) != 2 {
		return state
	}

	op := parts[0]
	val := parts[1]

	switch op {
	case "mul":
		if n, err := strconv.Atoi(val); err == nil {
			return new(big.Int).Mul(state, big.NewInt(int64(n)))
		}
	case "add":
		if n, err := strconv.Atoi(val); err == nil {
			return new(big.Int).Add(state, big.NewInt(int64(n)))
		}
	case "sub":
		if n, err := strconv.Atoi(val); err == nil {
			return new(big.Int).Sub(state, big.NewInt(int64(n)))
		}
	case "scale":
		if n, err := strconv.Atoi(val); err == nil {

			scaled := new(big.Int).Mul(state, big.NewInt(int64(n)))
			return scaled.Div(scaled, big.NewInt(100))
		}
	case "dom":

		h := sha256.Sum256([]byte(val))
		domainVal := new(big.Int).SetBytes(h[:8])
		return new(big.Int).Xor(state, domainVal)
	}

	return state
}

func affineTransform(state *big.Int, k, round int) *big.Int {

	a := big.NewInt(int64(6364136223846793005 + round*17))
	b := big.NewInt(int64(1442695040888963407 + round*31))

	result := new(big.Int).Mul(state, a)
	result.Add(result, b)

	mod := new(big.Int).Lsh(big.NewInt(1), uint(k))
	result.Mod(result, mod)

	return result
}

func computePressure(sc *SubstrateChain, state *big.Int) PressureMetrics {

	origin := GetWaveOrigin()

	hOut := Hadamard(origin)
	xOut := PauliX(origin)
	zOut := PauliZ(origin)

	return PressureMetrics{
		Hadamard: hOut.Amplitude,
		PauliX:   xOut.Amplitude,
		PauliZ:   zOut.Phase,
		Phase:    origin.Phase,
	}
}

func BuildPressureFromMesh(mesh *Mesh) PressureMetrics {

	origin := GetWaveOrigin()

	hOut := Hadamard(origin)
	xOut := PauliX(origin)
	zOut := PauliZ(origin)

	return PressureMetrics{
		Hadamard: hOut.Amplitude,
		PauliX:   xOut.Amplitude,
		PauliZ:   zOut.Phase,
		Phase:    origin.Phase,
	}
}

func (sc *SubstrateChain) Mint(data []byte) error {
	if sc == nil || sc.Chain == nil {
		return fmt.Errorf("substrate chain not initialized")
	}

	state := ReadState(sc.Chain.ID)

	block, err := EncodeBlock(data, state.Amplitude, state.Phase, len(sc.Chain.Blocks))
	if err != nil {
		return err
	}

	sc.Chain.AppendBlock(block)
	return nil
}

func (sc *SubstrateChain) MintEvent(eventType, details string) error {
	data := fmt.Sprintf("%s:%s", eventType, details)
	return sc.Mint([]byte(data))
}
