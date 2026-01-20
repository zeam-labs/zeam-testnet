

package node

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)


type NodeTransistor struct {
	MAC    []byte 
	NodeID int
}


func (n *NodeTransistor) Compute(payload []byte) []byte {
	data := append(n.MAC, payload...)
	return crypto.Keccak256(data)
}


func (n *NodeTransistor) ComputeChain(payload []byte, iterations int) [][]byte {
	results := make([][]byte, iterations)
	data := append(n.MAC, payload...)
	results[0] = crypto.Keccak256(data)

	for i := 1; i < iterations; i++ {
		data = append(results[i-1], payload...)
		results[i] = crypto.Keccak256(data)
	}

	return results
}


type DistributedComputer struct {
	nodes []*NodeTransistor

	NumNodes        int
	ChainIterations int

	mu             sync.Mutex
	totalOps       uint64
	broadcastsSent int
	solutionsFound int
}


func NewDistributedComputer(numNodes int, chainIterations int) *DistributedComputer {
	dc := &DistributedComputer{
		nodes:           make([]*NodeTransistor, numNodes),
		NumNodes:        numNodes,
		ChainIterations: chainIterations,
	}

	for i := 0; i < numNodes; i++ {
		dc.nodes[i] = &NodeTransistor{
			MAC:    crypto.Keccak256([]byte(fmt.Sprintf("l1_node_%d_ecdh_mac", i))),
			NodeID: i,
		}
	}

	return dc
}


func (dc *DistributedComputer) Broadcast(payload []byte) [][]byte {
	results := make([][]byte, 0, dc.NumNodes*dc.ChainIterations)

	for _, node := range dc.nodes {
		if dc.ChainIterations == 1 {
			results = append(results, node.Compute(payload))
		} else {
			chainResults := node.ComputeChain(payload, dc.ChainIterations)
			results = append(results, chainResults...)
		}
	}

	dc.mu.Lock()
	dc.broadcastsSent++
	dc.totalOps += uint64(dc.NumNodes * dc.ChainIterations * 24)
	dc.mu.Unlock()

	return results
}


func (dc *DistributedComputer) Stats() (broadcasts int, ops uint64, solutions int) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.broadcastsSent, dc.totalOps, dc.solutionsFound
}


func (dc *DistributedComputer) ExecuteTask(task ComputeTask, maxBroadcasts int) (interface{}, error) {
	for broadcast := 0; broadcast < maxBroadcasts; broadcast++ {
		payload := task.Encode()
		results := dc.Broadcast(payload)

		for nodeIdx, result := range results {
			nodeID := nodeIdx / max(dc.ChainIterations, 1)
			mac := dc.nodes[nodeID].MAC

			if task.ProcessResult(mac, result) {
				dc.mu.Lock()
				dc.solutionsFound++
				dc.mu.Unlock()
				return task.GetSolution(), nil
			}
		}
	}

	return nil, fmt.Errorf("no solution found in %d broadcasts", maxBroadcasts)
}


type ComputeTask interface {
	Encode() []byte
	ProcessResult(nodeMAC []byte, result []byte) bool
	GetSolution() interface{}
	Description() string
}


type FactoringTask struct {
	Target     *big.Int
	RangeStart uint64
	RangeSize  uint64
	Nonce      uint64

	mu     sync.Mutex
	factor *big.Int
	found  bool
}


func NewFactoringTask(target *big.Int, rangeStart, rangeSize uint64) *FactoringTask {
	return &FactoringTask{
		Target:     target,
		RangeStart: rangeStart,
		RangeSize:  rangeSize,
	}
}


func (ft *FactoringTask) Encode() []byte {
	payload := make([]byte, 56)
	nBytes := ft.Target.Bytes()
	copy(payload[32-len(nBytes):32], nBytes)
	binary.BigEndian.PutUint64(payload[32:40], ft.RangeStart)
	binary.BigEndian.PutUint64(payload[40:48], ft.RangeSize)
	binary.BigEndian.PutUint64(payload[48:56], ft.Nonce)
	ft.Nonce++
	return payload
}


func (ft *FactoringTask) ProcessResult(nodeMAC []byte, result []byte) bool {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	if ft.found {
		return true
	}

	for i := 0; i < 4; i++ {
		offset := binary.BigEndian.Uint64(result[i*8:(i+1)*8]) % ft.RangeSize
		candidate := ft.RangeStart + offset

		if candidate%2 == 0 && candidate != 2 {
			continue
		}
		if candidate < 2 {
			continue
		}

		d := new(big.Int).SetUint64(candidate)
		if d.Cmp(ft.Target) >= 0 {
			continue
		}

		remainder := new(big.Int).Mod(ft.Target, d)
		if remainder.Sign() == 0 {
			ft.factor = d
			ft.found = true
			return true
		}
	}

	return false
}


func (ft *FactoringTask) GetSolution() interface{} {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	return ft.factor
}


func (ft *FactoringTask) Description() string {
	return fmt.Sprintf("Factor %s (%d bits), range [%d, %d)",
		ft.Target.String(), ft.Target.BitLen(), ft.RangeStart, ft.RangeStart+ft.RangeSize)
}


func (ft *FactoringTask) AdvanceRange() {
	ft.RangeStart += ft.RangeSize
}


type StreamingFactorer struct {
	computer *DistributedComputer
	target   *big.Int

	RangeSize     uint64
	MaxCandidates uint64

	currentRange     uint64
	candidatesTested uint64
	factor           *big.Int
}


func NewStreamingFactorer(target *big.Int, numNodes, chainLen int, rangeSize uint64) *StreamingFactorer {
	return &StreamingFactorer{
		computer:  NewDistributedComputer(numNodes, chainLen),
		target:    target,
		RangeSize: rangeSize,
	}
}


func (sf *StreamingFactorer) Run(timeout time.Duration) (*big.Int, error) {
	start := time.Now()
	deadline := start.Add(timeout)

	sqrtN := new(big.Int).Sqrt(sf.target)
	var maxCandidate uint64
	if sqrtN.IsUint64() {
		maxCandidate = sqrtN.Uint64()
	} else {
		maxCandidate = 1 << 48
	}

	fmt.Println("========================================")
	fmt.Println("ZEAM STREAMING DISTRIBUTED FACTORER")
	fmt.Println("========================================")
	fmt.Printf("Target: %s (%d bits)\n", sf.target.String(), sf.target.BitLen())
	fmt.Printf("Nodes: %d, Chain: %d\n", sf.computer.NumNodes, sf.computer.ChainIterations)
	fmt.Printf("Range size: %d\n", sf.RangeSize)
	fmt.Printf("Max candidate: %d\n", maxCandidate)
	fmt.Printf("Timeout: %v\n", timeout)
	fmt.Println()

	sf.currentRange = 3

	for time.Now().Before(deadline) && sf.currentRange < maxCandidate {
		task := NewFactoringTask(sf.target, sf.currentRange, sf.RangeSize)
		payload := task.Encode()
		results := sf.computer.Broadcast(payload)

		for nodeIdx, result := range results {
			nodeID := nodeIdx / max(sf.computer.ChainIterations, 1)
			mac := sf.computer.nodes[nodeID].MAC

			if task.ProcessResult(mac, result) {
				sf.factor = task.GetSolution().(*big.Int)
				elapsed := time.Since(start)
				_, ops, _ := sf.computer.Stats()

				fmt.Printf("\n✓ FACTOR FOUND!\n")
				fmt.Printf("  Factor: %s\n", sf.factor.String())
				q := new(big.Int).Div(sf.target, sf.factor)
				fmt.Printf("  Other factor: %s\n", q.String())
				fmt.Printf("  Time: %v\n", elapsed)
				fmt.Printf("  Operations: %d\n", ops)
				fmt.Printf("  Rate: %.2e ops/sec\n", float64(ops)/elapsed.Seconds())

				return sf.factor, nil
			}
		}

		sf.currentRange += sf.RangeSize

		broadcasts, ops, _ := sf.computer.Stats()
		if broadcasts%100 == 0 {
			elapsed := time.Since(start)
			progress := float64(sf.currentRange) / float64(maxCandidate) * 100
			rate := float64(ops) / elapsed.Seconds()
			fmt.Printf("Progress: %.2f%% (range %d), %d ops, %.2e ops/sec\n",
				progress, sf.currentRange, ops, rate)
		}
	}

	elapsed := time.Since(start)
	_, ops, _ := sf.computer.Stats()
	return nil, fmt.Errorf("no factor found (checked up to %d, %d ops in %v)",
		sf.currentRange, ops, elapsed)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
