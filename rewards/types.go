package rewards

import (
	"crypto/ecdsa"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/libp2p/go-libp2p/core/peer"
	"zeam/content"
)

type StorageContributor struct {
	PeerID           peer.ID
	Address          common.Address
	BytesStored      uint64
	ByteHours        *big.Int
	ChunksStored     int
	ChallengesPassed int
	ChallengesFailed int
	RedundancyScore  float64
	JoinedAt         time.Time
	LastSeen         time.Time
}

type ChallengeRecord struct {
	ID           [32]byte
	ChunkID      content.ContentID
	Challenger   peer.ID
	Challenged   peer.ID
	Nonce        [32]byte
	ExpectedHash [32]byte
	IssuedAt     time.Time
	Deadline     time.Time
	Response     []byte
	Passed       bool
	Verified     bool
}

type ContributorScore struct {
	PeerID          peer.ID
	Address         common.Address
	ByteHours       *big.Int
	ChallengeRate   float64
	RedundancyBonus float64
	RawScore        float64
	CurvedScore     float64
	RewardWei       *big.Int
}

type MerkleLeaf struct {
	Address common.Address
	Amount  *big.Int
}

type EpochSnapshot struct {
	EpochNumber    uint64
	StartTime      time.Time
	EndTime        time.Time
	TotalRevenue   *big.Int
	TotalByteHours *big.Int
	Contributors   []ContributorScore
	MerkleRoot     [32]byte
	MerkleLeaves   []MerkleLeaf
}

type Config struct {

	EpochDuration time.Duration

	ChallengeFrequency    time.Duration
	ChallengeTimeout      time.Duration
	MinChallengesPerEpoch int

	OptimalBytesMin  uint64
	OptimalBytesMax  uint64
	SmallPenaltyRate float64
	WhalePenaltyRate float64

	MaxNetworkShare   float64
	RedundancyMinimum int

	FailedChallengeSlash float64

	ChallengeWeight   float64
	RedundancyWeight  float64

	PoolAddress common.Address
	OperatorKey *ecdsa.PrivateKey
}

func DefaultConfig() *Config {
	return &Config{

		EpochDuration: 24 * time.Hour,

		ChallengeFrequency:    15 * time.Minute,
		ChallengeTimeout:      30 * time.Second,
		MinChallengesPerEpoch: 10,

		OptimalBytesMin:  10 * 1024 * 1024 * 1024,
		OptimalBytesMax:  100 * 1024 * 1024 * 1024,
		SmallPenaltyRate: 0.5,
		WhalePenaltyRate: 0.3,

		MaxNetworkShare:   0.10,
		RedundancyMinimum: 3,

		FailedChallengeSlash: 0.10,

		ChallengeWeight:  0.20,
		RedundancyWeight: 0.10,
	}
}

func NewContributor(peerID peer.ID, address common.Address) *StorageContributor {
	return &StorageContributor{
		PeerID:    peerID,
		Address:   address,
		ByteHours: big.NewInt(0),
		JoinedAt:  time.Now(),
		LastSeen:  time.Now(),
	}
}

func (c *StorageContributor) ChallengePassRate() float64 {
	total := c.ChallengesPassed + c.ChallengesFailed
	if total == 0 {
		return 0
	}
	return float64(c.ChallengesPassed) / float64(total)
}
