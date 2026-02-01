package arb

import (
	"log"
	"math/big"
	"sync"
	"time"

	"zeam/node"
	"zeam/quantum"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type ZEAMDetector struct {
	mu sync.RWMutex

	transport *node.MultiChainTransport

	parser *MempoolParser

	pressure *quantum.PressureTracker

	priceFeed *WSPriceFeed

	shotgunPool   [32]byte
	shotgunThresh float64

	opportunities      map[[32]byte]*ScoredOpportunity
	opportunityTimeout time.Duration

	pendingImpacts map[uint64]map[common.Address]*PendingImpact

	config *ZEAMDetectorConfig

	swapsSeen       uint64
	opportunitiesFound uint64
	shotgunFiltered uint64
	executed        uint64

	OnOpportunity func(opp *ScoredOpportunity)

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

type ScoredOpportunity struct {
	*ArbitrageOpportunity

	FlowEntropy     [32]byte
	PressureScore   float64
	CoherenceScore  float64
	TimingScore     float64
	ShotgunPass     bool
	ShotgunValue    float64

	TriggerTx       common.Hash
	TriggerSwap     *PendingSwap
	PredictedImpact float64

	Priority        int
}

type PendingImpact struct {
	Pool            common.Address
	ChainID         uint64
	PendingSwaps    []*PendingSwap
	CumulativeImpact float64
	LastUpdate      time.Time
}

type ZEAMDetectorConfig struct {

	MinSpreadBPS        int64
	MinNetProfitWei     *big.Int
	MinPredictedImpact  float64

	SpreadWeight        float64
	PressureWeight      float64
	CoherenceWeight     float64
	TimingWeight        float64

	ShotgunEnabled      bool
	ShotgunThreshold    float64

	PressureDecayInterval time.Duration
	PressureDecayHalfLife time.Duration

	OpportunityTimeout  time.Duration
	ImpactWindow        time.Duration
}

func DefaultZEAMDetectorConfig() *ZEAMDetectorConfig {
	return &ZEAMDetectorConfig{
		MinSpreadBPS:        10,
		MinNetProfitWei:     big.NewInt(5e15),
		MinPredictedImpact:  5.0,

		SpreadWeight:        0.30,
		PressureWeight:      0.25,
		CoherenceWeight:     0.25,
		TimingWeight:        0.20,

		ShotgunEnabled:      true,
		ShotgunThreshold:    0.70,

		PressureDecayInterval: 1 * time.Second,
		PressureDecayHalfLife: 5 * time.Second,

		OpportunityTimeout:  10 * time.Second,
		ImpactWindow:        30 * time.Second,
	}
}

func NewZEAMDetector(transport *node.MultiChainTransport, priceFeed *WSPriceFeed, config *ZEAMDetectorConfig) *ZEAMDetector {
	if config == nil {
		config = DefaultZEAMDetectorConfig()
	}

	detector := &ZEAMDetector{
		transport:          transport,
		parser:             NewMempoolParser(),
		pressure:           quantum.NewPressureTracker(),
		priceFeed:          priceFeed,
		shotgunThresh:      config.ShotgunThreshold,
		opportunities:      make(map[[32]byte]*ScoredOpportunity),
		opportunityTimeout: config.OpportunityTimeout,
		pendingImpacts:     make(map[uint64]map[common.Address]*PendingImpact),
		config:             config,
		stopCh:             make(chan struct{}),
	}

	for _, chainID := range transport.GetEnabledChains() {
		detector.pendingImpacts[chainID] = make(map[common.Address]*PendingImpact)
	}

	return detector
}

func (zd *ZEAMDetector) Start() error {
	zd.mu.Lock()
	if zd.running {
		zd.mu.Unlock()
		return nil
	}
	zd.running = true
	zd.mu.Unlock()

	zd.transport.OnTxReceived = zd.handleTxHashes

	zd.parser.OnSwapDetected = zd.handlePendingSwap

	log.Println("[ZEAM] Detector started - watching mempool for pending swaps")
	return nil
}

func (zd *ZEAMDetector) Stop() {
	zd.mu.Lock()
	if !zd.running {
		zd.mu.Unlock()
		return
	}
	zd.running = false
	zd.mu.Unlock()

	log.Println("[ZEAM] Detector stopped")
}

func (zd *ZEAMDetector) handleTxHashes(chainID uint64, hashes []common.Hash) {

	zd.mu.Lock()
	for _, h := range hashes {
		for i := 0; i < 32; i++ {
			zd.shotgunPool[i] ^= h[i]
		}
	}
	zd.mu.Unlock()

}

func (zd *ZEAMDetector) handlePendingSwap(swap *PendingSwap) {
	zd.mu.Lock()
	zd.swapsSeen++
	zd.mu.Unlock()

	zd.cleanup()

	if swap.TokenIn == (common.Address{}) || swap.TokenOut == (common.Address{}) {
		return
	}

	log.Printf("[ZEAM] Pending swap detected: %s %s -> %s on %s (chain %d)",
		swap.TxHash.Hex()[:10], swap.TokenIn.Hex()[:10], swap.TokenOut.Hex()[:10],
		swap.Router, swap.ChainID)

	currentPrices := zd.priceFeed.GetCurrentPrices()

	predictedImpact := zd.estimateSwapImpact(swap)
	swap.EstimatedPriceImpact = predictedImpact

	zd.trackPendingImpact(swap)

	loc := quantum.BlockLocation{
		ChainID:  chainIDToString(swap.ChainID),
		BlockIdx: 0,
	}
	zd.pressure.AddPressure(loc, predictedImpact/100)

	opp := zd.detectOpportunity(swap, currentPrices, predictedImpact)
	if opp == nil {
		return
	}

	zd.scoreOpportunity(opp)

	if zd.config.ShotgunEnabled && !zd.applyShotgunFilter(opp) {
		zd.mu.Lock()
		zd.shotgunFiltered++
		zd.mu.Unlock()
		log.Printf("[ZEAM] Opportunity filtered by shotgun: %x", opp.ID[:4])
		return
	}

	zd.mu.Lock()
	zd.opportunities[opp.ID] = opp
	zd.opportunitiesFound++
	zd.mu.Unlock()

	log.Printf("[ZEAM] OPPORTUNITY FOUND: spread=%dbps, score=%.3f, priority=%d, impact=%.2fbps",
		opp.SpreadBPS, opp.Score, opp.Priority, predictedImpact)

	if zd.OnOpportunity != nil {
		zd.OnOpportunity(opp)
	}
}

func (zd *ZEAMDetector) estimateSwapImpact(swap *PendingSwap) float64 {
	if swap.AmountIn == nil {
		return 0
	}

	amountFloat := new(big.Float).SetInt(swap.AmountIn)
	amountEth, _ := new(big.Float).Quo(amountFloat, big.NewFloat(1e18)).Float64()

	impact := amountEth * (1 + amountEth/100)

	return impact
}

func (zd *ZEAMDetector) trackPendingImpact(swap *PendingSwap) {
	zd.mu.Lock()
	defer zd.mu.Unlock()

	chainImpacts := zd.pendingImpacts[swap.ChainID]
	if chainImpacts == nil {
		chainImpacts = make(map[common.Address]*PendingImpact)
		zd.pendingImpacts[swap.ChainID] = chainImpacts
	}

	impact := chainImpacts[swap.PoolAddress]
	if impact == nil {
		impact = &PendingImpact{
			Pool:         swap.PoolAddress,
			ChainID:      swap.ChainID,
			PendingSwaps: make([]*PendingSwap, 0),
		}
		chainImpacts[swap.PoolAddress] = impact
	}

	impact.PendingSwaps = append(impact.PendingSwaps, swap)
	impact.CumulativeImpact += swap.EstimatedPriceImpact
	impact.LastUpdate = time.Now()
}

func (zd *ZEAMDetector) detectOpportunity(swap *PendingSwap, prices map[uint64]map[string]*big.Float, predictedImpact float64) *ScoredOpportunity {

	if predictedImpact < zd.config.MinPredictedImpact {
		return nil
	}

	for _, buyChain := range zd.transport.GetEnabledChains() {
		for _, sellChain := range zd.transport.GetEnabledChains() {
			if buyChain == sellChain && swap.ChainID != buyChain {
				continue
			}

			buyPrices := prices[buyChain]
			sellPrices := prices[sellChain]
			if buyPrices == nil || sellPrices == nil {
				continue
			}

			buyPrice := buyPrices["ETH-USDC"]
			sellPrice := sellPrices["ETH-USDC"]
			if buyPrice == nil || sellPrice == nil {
				continue
			}

			adjustedBuyPrice := buyPrice
			if swap.ChainID == buyChain {

				impactMultiplier := 1.0 - (predictedImpact / 10000)
				adjustedBuyPrice = new(big.Float).Mul(buyPrice, big.NewFloat(impactMultiplier))
			}

			spread := new(big.Float).Quo(sellPrice, adjustedBuyPrice)
			spreadFloat, _ := spread.Float64()
			spreadBPS := int64((spreadFloat - 1.0) * 10000)

			if spreadBPS < zd.config.MinSpreadBPS {
				continue
			}

			opp := &ScoredOpportunity{
				ArbitrageOpportunity: &ArbitrageOpportunity{
					Type:         OpTypeCrossChainSame,
					Status:       StatusDetected,
					BuyPrice:     adjustedBuyPrice,
					SellPrice:    sellPrice,
					SpreadBPS:    spreadBPS,
					BuyChain:     buyChain,
					SellChain:    sellChain,
					IsCrossChain: buyChain != sellChain,
					DetectedAt:   time.Now(),
					ExpiresAt:    time.Now().Add(zd.opportunityTimeout),
				},
				TriggerTx:       swap.TxHash,
				TriggerSwap:     swap,
				PredictedImpact: predictedImpact,
			}
			opp.ComputeID()

			return opp
		}
	}

	return nil
}

func (zd *ZEAMDetector) scoreOpportunity(opp *ScoredOpportunity) {

	maxSpreadBPS := int64(100)
	spreadScore := float64(opp.SpreadBPS) / float64(maxSpreadBPS)
	if spreadScore > 1.0 {
		spreadScore = 1.0
	}

	totalPressure := zd.pressure.GetTotalPressure()
	maxPressure := 100.0
	pressureScore := totalPressure / maxPressure
	if pressureScore > 1.0 {
		pressureScore = 1.0
	}
	opp.PressureScore = pressureScore

	hotspots := zd.pressure.GetHotspots(0.3)
	chainsWithPressure := make(map[string]bool)
	for _, hs := range hotspots {
		chainsWithPressure[hs.Location.ChainID] = true
	}
	coherenceScore := float64(len(chainsWithPressure)) / float64(len(zd.transport.GetEnabledChains()))
	opp.CoherenceScore = coherenceScore

	flowRate := zd.transport.GetFlowRate()

	maxFlowRate := 1000.0
	timingScore := flowRate / maxFlowRate
	if timingScore > 1.0 {
		timingScore = 1.0
	}
	opp.TimingScore = timingScore

	opp.FlowEntropy = [32]byte{}
	entropy := zd.transport.GetUnifiedEntropy()
	copy(opp.FlowEntropy[:], entropy)

	opp.Score = zd.config.SpreadWeight*spreadScore +
		zd.config.PressureWeight*pressureScore +
		zd.config.CoherenceWeight*coherenceScore +
		zd.config.TimingWeight*timingScore

	opp.Priority = int(opp.Score * 1000)

	opp.Pressure = PressureMetrics{
		Hadamard: pressureScore,
		PauliX: coherenceScore,
		PauliZ:   opp.TimingScore,
		Phase:   spreadScore,
	}
}

func (zd *ZEAMDetector) applyShotgunFilter(opp *ScoredOpportunity) bool {

	oppHash := ComputeOpportunityHash(opp.TriggerSwap)

	zd.mu.RLock()
	var result [32]byte
	for i := 0; i < 32; i++ {
		result[i] = oppHash[i] ^ zd.shotgunPool[i]
	}
	zd.mu.RUnlock()

	resultBig := new(big.Int).SetBytes(result[:])
	maxBig := new(big.Int).Lsh(big.NewInt(1), 256)
	maxBig.Sub(maxBig, big.NewInt(1))

	normalized := new(big.Float).SetInt(resultBig)
	normalized.Quo(normalized, new(big.Float).SetInt(maxBig))
	normalizedFloat, _ := normalized.Float64()

	opp.ShotgunValue = normalizedFloat
	opp.ShotgunPass = normalizedFloat < zd.shotgunThresh

	return opp.ShotgunPass
}

func (zd *ZEAMDetector) cleanup() {
	now := time.Now()

	zd.mu.Lock()
	defer zd.mu.Unlock()

	for id, opp := range zd.opportunities {
		if now.After(opp.ExpiresAt) {
			delete(zd.opportunities, id)
		}
	}

	cutoff := now.Add(-zd.config.ImpactWindow)
	for chainID, impacts := range zd.pendingImpacts {
		for pool, impact := range impacts {
			if impact.LastUpdate.Before(cutoff) {
				delete(zd.pendingImpacts[chainID], pool)
			}
		}
	}

	zd.parser.CleanOldSwaps(5 * time.Minute)
}

func (zd *ZEAMDetector) InjectTransaction(chainID uint64, tx *types.Transaction) {
	zd.parser.ParseTransaction(chainID, tx)
}

func (zd *ZEAMDetector) GetTopOpportunities(n int) []*ScoredOpportunity {
	zd.mu.RLock()
	defer zd.mu.RUnlock()

	opps := make([]*ScoredOpportunity, 0, len(zd.opportunities))
	for _, opp := range zd.opportunities {
		opps = append(opps, opp)
	}

	for i := 0; i < len(opps); i++ {
		for j := i + 1; j < len(opps); j++ {
			if opps[j].Priority > opps[i].Priority {
				opps[i], opps[j] = opps[j], opps[i]
			}
		}
	}

	if n > len(opps) {
		n = len(opps)
	}
	return opps[:n]
}

func (zd *ZEAMDetector) Stats() map[string]interface{} {
	zd.mu.RLock()
	defer zd.mu.RUnlock()

	return map[string]interface{}{
		"swaps_seen":          zd.swapsSeen,
		"opportunities_found": zd.opportunitiesFound,
		"shotgun_filtered":    zd.shotgunFiltered,
		"executed":            zd.executed,
		"active_opportunities": len(zd.opportunities),
		"total_pressure":      zd.pressure.GetTotalPressure(),
		"pressure_events":     zd.pressure.GetEventCount(),
		"parser_stats":        zd.parser.Stats(),
	}
}

func chainIDToString(chainID uint64) string {
	switch chainID {
	case ChainIDEthereum:
		return "ethereum"
	case ChainIDBase:
		return "base"
	case ChainIDOptimism:
		return "optimism"
	default:
		return "unknown"
	}
}
