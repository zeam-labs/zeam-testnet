package rewards

import (
	"math"
	"math/big"
)

type MiddleOutCurve struct {
	OptimalMin   uint64
	OptimalMax   uint64
	SmallPenalty float64
	WhalePenalty float64
}

func NewMiddleOutCurve(cfg *Config) *MiddleOutCurve {
	return &MiddleOutCurve{
		OptimalMin:   cfg.OptimalBytesMin,
		OptimalMax:   cfg.OptimalBytesMax,
		SmallPenalty: cfg.SmallPenaltyRate,
		WhalePenalty: cfg.WhalePenaltyRate,
	}
}

func (c *MiddleOutCurve) Efficiency(bytes uint64) float64 {
	if bytes == 0 {
		return 0
	}

	if bytes >= c.OptimalMin && bytes <= c.OptimalMax {
		return 1.0
	}

	if bytes < c.OptimalMin {

		ratio := float64(bytes) / float64(c.OptimalMin)
		return c.SmallPenalty + (1.0-c.SmallPenalty)*ratio
	}

	excessRatio := float64(bytes) / float64(c.OptimalMax)

	decay := 1.0 / (1.0 + math.Log(excessRatio))
	return c.WhalePenalty + (1.0-c.WhalePenalty)*decay
}

func (c *MiddleOutCurve) ComputeRewards(
	contributors []StorageContributor,
	totalRevenue *big.Int,
	cfg *Config,
) []ContributorScore {
	if len(contributors) == 0 {
		return nil
	}

	scores := make([]ContributorScore, len(contributors))
	totalCurvedScore := float64(0)

	for i, contrib := range contributors {
		scores[i].PeerID = contrib.PeerID
		scores[i].Address = contrib.Address
		scores[i].ByteHours = new(big.Int).Set(contrib.ByteHours)

		scores[i].ChallengeRate = contrib.ChallengePassRate()

		scores[i].RedundancyBonus = contrib.RedundancyScore

		byteHoursFloat, _ := new(big.Float).SetInt(contrib.ByteHours).Float64()

		baseWeight := 1.0 - cfg.ChallengeWeight - cfg.RedundancyWeight
		weightedScore := baseWeight +
			cfg.ChallengeWeight*scores[i].ChallengeRate +
			cfg.RedundancyWeight*scores[i].RedundancyBonus

		scores[i].RawScore = byteHoursFloat * weightedScore

		if contrib.ChallengesFailed > 0 {
			penaltyMultiplier := math.Pow(1.0-cfg.FailedChallengeSlash, float64(contrib.ChallengesFailed))
			scores[i].RawScore *= penaltyMultiplier
		}

		efficiency := c.Efficiency(contrib.BytesStored)
		scores[i].CurvedScore = scores[i].RawScore * efficiency

		totalCurvedScore += scores[i].CurvedScore
	}

	if totalCurvedScore > 0 && totalRevenue.Sign() > 0 {
		revFloat, _ := new(big.Float).SetInt(totalRevenue).Float64()
		for i := range scores {
			share := scores[i].CurvedScore / totalCurvedScore
			rewardFloat := revFloat * share
			scores[i].RewardWei = new(big.Int)
			new(big.Float).SetFloat64(rewardFloat).Int(scores[i].RewardWei)
		}
	} else {

		for i := range scores {
			scores[i].RewardWei = big.NewInt(0)
		}
	}

	return scores
}

func ApplyConcentrationCaps(scores []ContributorScore, maxShare float64) {
	if len(scores) == 0 || maxShare <= 0 || maxShare >= 1 {
		return
	}

	totalRewards := big.NewInt(0)
	for _, s := range scores {
		totalRewards.Add(totalRewards, s.RewardWei)
	}

	if totalRewards.Sign() == 0 {
		return
	}

	maxRewardFloat := new(big.Float).Mul(
		new(big.Float).SetInt(totalRewards),
		new(big.Float).SetFloat64(maxShare),
	)
	maxReward, _ := maxRewardFloat.Int(nil)

	excess := big.NewInt(0)
	cappedIndices := make(map[int]bool)
	for i := range scores {
		if scores[i].RewardWei.Cmp(maxReward) > 0 {
			diff := new(big.Int).Sub(scores[i].RewardWei, maxReward)
			excess.Add(excess, diff)
			scores[i].RewardWei.Set(maxReward)
			cappedIndices[i] = true
		}
	}

	if excess.Sign() == 0 {
		return
	}

	nonCappedTotal := big.NewInt(0)
	for i := range scores {
		if !cappedIndices[i] {
			nonCappedTotal.Add(nonCappedTotal, scores[i].RewardWei)
		}
	}

	if nonCappedTotal.Sign() == 0 {
		return
	}

	for i := range scores {
		if cappedIndices[i] {
			continue
		}

		share := new(big.Int).Mul(scores[i].RewardWei, excess)
		share.Div(share, nonCappedTotal)
		scores[i].RewardWei.Add(scores[i].RewardWei, share)
	}
}
