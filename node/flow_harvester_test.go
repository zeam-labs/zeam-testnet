package node

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func TestMempoolFlowIngest(t *testing.T) {
	flow := NewMempoolFlow(100)

	
	hashes := []common.Hash{
		common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
		common.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
	}

	flow.Ingest(hashes)

	count, _, _ := flow.Stats()
	if count != 3 {
		t.Errorf("Expected 3 hashes, got %d", count)
	}

	
	entropy := flow.Entropy()
	if len(entropy) != 32 {
		t.Errorf("Expected 32 bytes entropy, got %d", len(entropy))
	}

	
	allZero := true
	for _, b := range entropy {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("Entropy should not be all zeros")
	}
}

func TestMempoolFlowRecentHashes(t *testing.T) {
	flow := NewMempoolFlow(5) 

	
	for i := 0; i < 10; i++ {
		h := common.Hash{}
		h[0] = byte(i)
		flow.Ingest([]common.Hash{h})
	}

	
	recent := flow.RecentHashes(5)
	if len(recent) != 5 {
		t.Errorf("Expected 5 recent hashes, got %d", len(recent))
	}

	
	if recent[0][0] != 9 {
		t.Errorf("Expected most recent hash[0]=9, got %d", recent[0][0])
	}
}

func TestFlowCompute(t *testing.T) {
	flow := NewMempoolFlow(100)

	
	for i := 0; i < 50; i++ {
		h := common.Hash{}
		h[0] = byte(i)
		h[1] = byte(i * 2)
		flow.Ingest([]common.Hash{h})
	}

	compute := NewFlowCompute(flow)

	
	bytes := compute.NextBytes(16)
	if len(bytes) != 16 {
		t.Errorf("Expected 16 bytes, got %d", len(bytes))
	}

	
	val := compute.NextUint64()
	
	t.Logf("NextUint64: %d", val)

	
	for i := 0; i < 100; i++ {
		idx := compute.SelectIndex(10)
		if idx < 0 || idx >= 10 {
			t.Errorf("SelectIndex(10) returned %d, expected [0,10)", idx)
		}
	}
}

func TestFlowComputeDeterminism(t *testing.T) {
	
	flow1 := NewMempoolFlow(100)
	flow2 := NewMempoolFlow(100)

	hashes := []common.Hash{
		common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
	}

	flow1.Ingest(hashes)
	flow2.Ingest(hashes)

	e1 := flow1.Entropy()
	e2 := flow2.Entropy()

	for i := range e1 {
		if e1[i] != e2[i] {
			t.Errorf("Entropy mismatch at byte %d: %x vs %x", i, e1[i], e2[i])
		}
	}
}

func TestFlowHarvesterStats(t *testing.T) {
	
	flow := NewMempoolFlow(100)

	
	for i := 0; i < 100; i++ {
		h := common.Hash{}
		h[0] = byte(i)
		flow.Ingest([]common.Hash{h})
		time.Sleep(time.Millisecond)
	}

	count, rate, lastHash := flow.Stats()
	if count != 100 {
		t.Errorf("Expected 100 hashes, got %d", count)
	}
	t.Logf("Flow rate: %.2f hashes/sec", rate)
	t.Logf("Last hash: %v", lastHash)
}

func TestFlowWordSelector(t *testing.T) {
	flow := NewMempoolFlow(100)

	
	for i := 0; i < 100; i++ {
		h := common.Hash{}
		for j := 0; j < 32; j++ {
			h[j] = byte((i * 7 + j * 13) % 256)
		}
		flow.Ingest([]common.Hash{h})
	}

	compute := NewFlowCompute(flow)
	selector := NewFlowWordSelector(compute)

	candidates := []string{"apple", "banana", "cherry", "date", "elderberry"}

	
	for i := 0; i < 20; i++ {
		word := selector.Select(candidates)
		found := false
		for _, c := range candidates {
			if c == word {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Selected word %q not in candidates", word)
		}
	}
}

func TestFlowWeightedSelection(t *testing.T) {
	flow := NewMempoolFlow(100)

	
	for i := 0; i < 100; i++ {
		h := common.Hash{}
		h[0] = byte(i)
		flow.Ingest([]common.Hash{h})
	}

	compute := NewFlowCompute(flow)
	selector := NewFlowWordSelector(compute)

	candidates := []string{"common", "rare", "very_rare"}
	weights := []float64{0.9, 0.09, 0.01}

	
	counts := make(map[string]int)
	for i := 0; i < 1000; i++ {
		word := selector.SelectWeighted(candidates, weights)
		counts[word]++
	}

	t.Logf("Distribution: %v", counts)

	
	if counts["common"] < counts["rare"] {
		t.Logf("Warning: common appeared less than rare (probabilistic)")
	}
}
