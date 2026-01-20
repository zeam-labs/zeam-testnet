package quantum

import (
	"encoding/json"
	"sync"
)


type Block struct {
	Header      BlockHeader
	Body        []byte 
	CrossAnchor []byte 
}


type BlockHeader struct {
	Amplitude   float64 
	Phase       float64 
	SynsetIndex int     
}


type Chain struct {
	ID       string
	Diameter int      
	Gates    []Gate   
	Blocks   []Block  
	Anchors  []string 
}


type BlockLocation struct {
	ChainID  string
	BlockIdx int
}


var CurrentBlock map[string]BlockLocation
var currentMu sync.RWMutex


func InitCurrentBlock() {
	currentMu.Lock()
	defer currentMu.Unlock()
	CurrentBlock = make(map[string]BlockLocation)
}


func SetCurrentBlock(chainID string, loc BlockLocation) {
	currentMu.Lock()
	defer currentMu.Unlock()
	CurrentBlock[chainID] = loc
}


func GetCurrentBlock(chainID string) (BlockLocation, bool) {
	currentMu.RLock()
	defer currentMu.RUnlock()
	loc, exists := CurrentBlock[chainID]
	return loc, exists
}


func AllCurrentBlocks() map[string]BlockLocation {
	currentMu.RLock()
	defer currentMu.RUnlock()

	snapshot := make(map[string]BlockLocation)
	for k, v := range CurrentBlock {
		snapshot[k] = v
	}
	return snapshot
}


func Junction(flows ...[]byte) []byte {
	if len(flows) == 0 {
		return []byte{}
	}

	maxLen := 0
	for _, f := range flows {
		if len(f) > maxLen {
			maxLen = len(f)
		}
	}

	result := make([]byte, maxLen)

	
	for _, f := range flows {
		for i := 0; i < len(f); i++ {
			result[i] ^= f[i]
		}
	}

	return result
}


func EncodeBlock(data interface{}, amplitude, phase float64, synsetIndex int) (Block, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return Block{}, err
	}

	return Block{
		Header: BlockHeader{
			Amplitude:   amplitude,
			Phase:       phase,
			SynsetIndex: synsetIndex,
		},
		Body: body,
	}, nil
}


func DecodeBlock(block Block, target interface{}) error {
	return json.Unmarshal(block.Body, target)
}


func (c *Chain) AppendBlock(block Block) {
	c.Blocks = append(c.Blocks, block)
}


func (c *Chain) GetBlock(idx int) (Block, bool) {
	if idx < 0 || idx >= len(c.Blocks) {
		return Block{}, false
	}
	return c.Blocks[idx], true
}


func SetCrossAnchors(chains map[string]*Chain) {
	maxBlocks := 0
	for _, chain := range chains {
		if len(chain.Blocks) > maxBlocks {
			maxBlocks = len(chain.Blocks)
		}
	}

	for blockIdx := 0; blockIdx < maxBlocks; blockIdx++ {
		var blockData [][]byte
		for _, chain := range chains {
			if blockIdx < len(chain.Blocks) {
				blockData = append(blockData, chain.Blocks[blockIdx].Body)
			}
		}

		if len(blockData) > 1 {
			junction := Junction(blockData...)
			for _, chain := range chains {
				if blockIdx < len(chain.Blocks) {
					chain.Blocks[blockIdx].CrossAnchor = junction
				}
			}
		}
	}
}
