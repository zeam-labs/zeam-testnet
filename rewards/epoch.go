package rewards

import (
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"zeam/content"
)

type EpochManager struct {
	mu sync.RWMutex

	currentEpoch       uint64
	epochStart         time.Time
	accumulatedRevenue *big.Int

	contributors map[peer.ID]*StorageContributor

	history []EpochSnapshot

	curve     *MiddleOutCurve
	providers *content.ProviderRegistry
	store     *content.Store
	config    *Config

	OnEpochEnd func(snapshot *EpochSnapshot)

	ctx    chan struct{}
	wg     sync.WaitGroup
}

func NewEpochManager(
	providers *content.ProviderRegistry,
	store *content.Store,
	cfg *Config,
) *EpochManager {
	return &EpochManager{
		currentEpoch:       0,
		epochStart:         time.Now(),
		accumulatedRevenue: big.NewInt(0),
		contributors:       make(map[peer.ID]*StorageContributor),
		curve:              NewMiddleOutCurve(cfg),
		providers:          providers,
		store:              store,
		config:             cfg,
		ctx:                make(chan struct{}),
	}
}

func (em *EpochManager) Start() {
	em.wg.Add(1)
	go em.epochLoop()
}

func (em *EpochManager) epochLoop() {
	defer em.wg.Done()

	ticker := time.NewTicker(em.config.EpochDuration)
	defer ticker.Stop()

	for {
		select {
		case <-em.ctx:
			return
		case <-ticker.C:
			em.FinalizeEpoch()
		}
	}
}

func (em *EpochManager) AddRevenue(amount *big.Int) {
	if amount == nil || amount.Sign() <= 0 {
		return
	}
	em.mu.Lock()
	defer em.mu.Unlock()
	em.accumulatedRevenue.Add(em.accumulatedRevenue, amount)
}

func (em *EpochManager) GetAccumulatedRevenue() *big.Int {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return new(big.Int).Set(em.accumulatedRevenue)
}

func (em *EpochManager) UpdateContributor(peerID peer.ID, address common.Address, bytesStored uint64, chunksStored int) {
	em.mu.Lock()
	defer em.mu.Unlock()

	contrib, ok := em.contributors[peerID]
	if !ok {
		contrib = NewContributor(peerID, address)
		em.contributors[peerID] = contrib
	}

	contrib.BytesStored = bytesStored
	contrib.ChunksStored = chunksStored
	contrib.LastSeen = time.Now()
	contrib.Address = address
}

func (em *EpochManager) RecordChallengeResult(peerID peer.ID, passed bool) {
	em.mu.Lock()
	defer em.mu.Unlock()

	contrib, ok := em.contributors[peerID]
	if !ok {
		return
	}

	if passed {
		contrib.ChallengesPassed++
	} else {
		contrib.ChallengesFailed++
	}
}

func (em *EpochManager) FinalizeEpoch() *EpochSnapshot {
	em.mu.Lock()

	epochDuration := time.Since(em.epochStart)
	hours := epochDuration.Hours()

	for _, contrib := range em.contributors {

		byteHours := new(big.Int).Mul(
			big.NewInt(int64(contrib.BytesStored)),
			big.NewInt(int64(hours*1000)),
		)
		byteHours.Div(byteHours, big.NewInt(1000))
		contrib.ByteHours.Add(contrib.ByteHours, byteHours)
	}

	em.calculateRedundancyScores()

	contribList := make([]StorageContributor, 0, len(em.contributors))
	for _, c := range em.contributors {
		contribList = append(contribList, *c)
	}

	scores := em.curve.ComputeRewards(contribList, em.accumulatedRevenue, em.config)

	ApplyConcentrationCaps(scores, em.config.MaxNetworkShare)

	leaves := make([]MerkleLeaf, 0, len(scores))
	for _, s := range scores {
		if s.RewardWei.Sign() > 0 {
			leaves = append(leaves, MerkleLeaf{
				Address: s.Address,
				Amount:  s.RewardWei,
			})
		}
	}
	merkleRoot := ComputeMerkleRoot(leaves)

	totalByteHours := big.NewInt(0)
	for _, c := range em.contributors {
		totalByteHours.Add(totalByteHours, c.ByteHours)
	}

	snapshot := EpochSnapshot{
		EpochNumber:    em.currentEpoch,
		StartTime:      em.epochStart,
		EndTime:        time.Now(),
		TotalRevenue:   new(big.Int).Set(em.accumulatedRevenue),
		TotalByteHours: totalByteHours,
		Contributors:   scores,
		MerkleRoot:     merkleRoot,
		MerkleLeaves:   leaves,
	}

	em.history = append(em.history, snapshot)

	em.currentEpoch++
	em.epochStart = time.Now()
	em.accumulatedRevenue = big.NewInt(0)

	for _, c := range em.contributors {
		c.ByteHours = big.NewInt(0)
	}

	em.mu.Unlock()

	if em.OnEpochEnd != nil {
		em.OnEpochEnd(&snapshot)
	}

	fmt.Printf("[Rewards] Epoch %d finalized: %d contributors, %s wei distributed\n",
		snapshot.EpochNumber, len(snapshot.Contributors), snapshot.TotalRevenue)

	return &snapshot
}

func (em *EpochManager) calculateRedundancyScores() {
	if em.providers == nil {
		return
	}

	allProviders := em.providers.All()
	if len(allProviders) == 0 {
		return
	}

	chunkProviderCount := make(map[content.ContentID]int)
	for root, records := range allProviders {
		chunkProviderCount[root] = len(records)
	}

	for _, contrib := range em.contributors {
		totalScore := float64(0)
		chunksChecked := 0

		for root, records := range allProviders {

			provides := false
			for _, r := range records {
				if r.PeerID == contrib.PeerID {
					provides = true
					break
				}
			}

			if provides {
				count := chunkProviderCount[root]
				if count < em.config.RedundancyMinimum {

					totalScore += 1.0 / float64(count)
				}
				chunksChecked++
			}
		}

		if chunksChecked > 0 {
			contrib.RedundancyScore = totalScore / float64(chunksChecked)
		}
	}
}

func (em *EpochManager) CurrentEpoch() uint64 {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return em.currentEpoch
}

func (em *EpochManager) ContributorCount() int {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return len(em.contributors)
}

func (em *EpochManager) GetHistory() []EpochSnapshot {
	em.mu.RLock()
	defer em.mu.RUnlock()
	result := make([]EpochSnapshot, len(em.history))
	copy(result, em.history)
	return result
}

func (em *EpochManager) Stop() {
	close(em.ctx)
	em.wg.Wait()
}

func ComputeMerkleRoot(leaves []MerkleLeaf) [32]byte {
	if len(leaves) == 0 {
		return [32]byte{}
	}

	sort.Slice(leaves, func(i, j int) bool {
		return leaves[i].Address.Hex() < leaves[j].Address.Hex()
	})

	hashes := make([][]byte, len(leaves))
	for i, leaf := range leaves {
		data := append(leaf.Address.Bytes(), common.LeftPadBytes(leaf.Amount.Bytes(), 32)...)
		hashes[i] = crypto.Keccak256(data)
	}

	for len(hashes) > 1 {
		var nextLevel [][]byte
		for i := 0; i < len(hashes); i += 2 {
			if i+1 < len(hashes) {

				left, right := hashes[i], hashes[i+1]
				if string(left) > string(right) {
					left, right = right, left
				}
				nextLevel = append(nextLevel, crypto.Keccak256(append(left, right...)))
			} else {

				nextLevel = append(nextLevel, hashes[i])
			}
		}
		hashes = nextLevel
	}

	var root [32]byte
	copy(root[:], hashes[0])
	return root
}

func ComputeMerkleProof(leaves []MerkleLeaf, address common.Address) ([][]byte, error) {
	if len(leaves) == 0 {
		return nil, fmt.Errorf("no leaves")
	}

	sort.Slice(leaves, func(i, j int) bool {
		return leaves[i].Address.Hex() < leaves[j].Address.Hex()
	})

	targetIdx := -1
	for i, leaf := range leaves {
		if leaf.Address == address {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		return nil, fmt.Errorf("address not in leaves")
	}

	hashes := make([][]byte, len(leaves))
	for i, leaf := range leaves {
		data := append(leaf.Address.Bytes(), common.LeftPadBytes(leaf.Amount.Bytes(), 32)...)
		hashes[i] = crypto.Keccak256(data)
	}

	var proof [][]byte
	idx := targetIdx

	for len(hashes) > 1 {
		var nextLevel [][]byte
		for i := 0; i < len(hashes); i += 2 {
			if i+1 < len(hashes) {
				left, right := hashes[i], hashes[i+1]
				if string(left) > string(right) {
					left, right = right, left
				}

				if i == idx || i+1 == idx {
					if i == idx {
						proof = append(proof, hashes[i+1])
					} else {
						proof = append(proof, hashes[i])
					}
					idx = len(nextLevel)
				}

				nextLevel = append(nextLevel, crypto.Keccak256(append(left, right...)))
			} else {

				if i == idx {
					idx = len(nextLevel)
				}
				nextLevel = append(nextLevel, hashes[i])
			}
		}
		hashes = nextLevel
	}

	return proof, nil
}
