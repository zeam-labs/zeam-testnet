package arb

import (
	"math"
	"math/big"
	"sync"
	"time"

	"zeam/node"
)

type OpportunityDetector struct {
	mu sync.RWMutex

	priceFeed *PriceFeed

	shotgun *ShotgunFilter

	opportunities map[[32]byte]*ArbitrageOpportunity

	config *OpportunityConfig

	OnOpportunity func(*ArbitrageOpportunity)

	currentPressure  PressureMetrics
	pressureMu       sync.RWMutex

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

type OpportunityConfig struct {
	MinSpreadBPS      int64
	MinProfitWei      *big.Int
	MaxOpportunityAge time.Duration
	MaxConcurrent     int
	EnableShotgun     bool
	ShotgunThreshold  float64
}

func DefaultOpportunityConfig() *OpportunityConfig {
	return &OpportunityConfig{
		MinSpreadBPS:      10,
		MinProfitWei:      big.NewInt(1e16),
		MaxOpportunityAge: 30 * time.Second,
		MaxConcurrent:     100,
		EnableShotgun:     true,
		ShotgunThreshold:  0.7,
	}
}

func NewOpportunityDetector(priceFeed *PriceFeed, config *OpportunityConfig) *OpportunityDetector {
	if config == nil {
		config = DefaultOpportunityConfig()
	}

	od := &OpportunityDetector{
		priceFeed:     priceFeed,
		opportunities: make(map[[32]byte]*ArbitrageOpportunity),
		config:        config,
		stopCh:        make(chan struct{}),
	}

	if config.EnableShotgun {
		od.shotgun = NewShotgunFilter(1000, config.ShotgunThreshold)
	}

	return od
}

func (od *OpportunityDetector) Start() error {
	od.mu.Lock()
	if od.running {
		od.mu.Unlock()
		return nil
	}
	od.running = true
	od.mu.Unlock()

	od.priceFeed.OnSpreadAlert = od.handleSpreadAlert

	return nil
}

func (od *OpportunityDetector) Stop() {
	od.mu.Lock()
	if !od.running {
		od.mu.Unlock()
		return
	}
	od.running = false
	od.mu.Unlock()
}

func (od *OpportunityDetector) scan() {
	spreads := od.priceFeed.GetAllSpreads()

	for pairID, spread := range spreads {
		if spread.SpreadBPS < od.config.MinSpreadBPS {
			continue
		}

		od.evaluateOpportunity(pairID, spread)
	}
}

func (od *OpportunityDetector) handleSpreadAlert(pairID string, buyChain, sellChain uint64, spreadBPS int64) {
	if spreadBPS < od.config.MinSpreadBPS {
		return
	}

	od.pruneExpired()

	buy, sell, spread := od.priceFeed.GetBestBuySell(pairID)
	if buy == nil || sell == nil {
		return
	}

	spreadInfo := &SpreadInfo{
		PairID:    pairID,
		BuyChain:  buy.ChainID,
		SellChain: sell.ChainID,
		BuyPrice:  buy.Price,
		SellPrice: sell.Price,
		SpreadBPS: spread,
		BuyDEX:    buy.DEX,
		SellDEX:   sell.DEX,
		Timestamp: time.Now(),
	}

	od.evaluateOpportunity(pairID, spreadInfo)
}

func (od *OpportunityDetector) evaluateOpportunity(pairID string, spread *SpreadInfo) {

	buyPrice := od.priceFeed.GetLatestPrice(pairID, spread.BuyChain)
	sellPrice := od.priceFeed.GetLatestPrice(pairID, spread.SellChain)

	if buyPrice == nil || sellPrice == nil {
		return
	}

	opp := &ArbitrageOpportunity{
		Type:         OpTypeCrossChainSame,
		Status:       StatusDetected,
		BuyPrice:     buyPrice.Price,
		SellPrice:    sellPrice.Price,
		SpreadBPS:    spread.SpreadBPS,
		BuyChain:     spread.BuyChain,
		SellChain:    spread.SellChain,
		IsCrossChain: spread.BuyChain != spread.SellChain,
		DetectedAt:   time.Now(),
		ExpiresAt:    time.Now().Add(od.config.MaxOpportunityAge),
	}

	opp.Path = []PathStep{
		{
			ChainID:  spread.BuyChain,
			Action:   ActionSwap,
			DEX:      spread.BuyDEX,
			Pool:     buyPrice.PoolAddress,
			TokenIn:  buyPrice.Pair.Token0.Address,
			TokenOut: buyPrice.Pair.Token1.Address,
		},
		{
			ChainID:  spread.SellChain,
			Action:   ActionSwap,
			DEX:      spread.SellDEX,
			Pool:     sellPrice.PoolAddress,
			TokenIn:  sellPrice.Pair.Token1.Address,
			TokenOut: sellPrice.Pair.Token0.Address,
		},
	}

	opp.ComputeID()

	if od.shotgun != nil && od.config.EnableShotgun {
		accepted, score := od.shotgun.AcceptWithScore(opp.ID)
		if !accepted {
			return
		}

		opp.Score = score * 0.2
	}

	od.scoreOpportunity(opp)

	if opp.Score < 0.15 {
		return
	}

	od.mu.Lock()
	defer od.mu.Unlock()

	if _, exists := od.opportunities[opp.ID]; exists {
		return
	}

	if len(od.opportunities) >= od.config.MaxConcurrent {

		od.removeLowestScored()
	}

	opp.Status = StatusScored
	od.opportunities[opp.ID] = opp

	if od.OnOpportunity != nil {
		go od.OnOpportunity(opp)
	}
}

func (od *OpportunityDetector) scoreOpportunity(opp *ArbitrageOpportunity) {
	od.pressureMu.RLock()
	pressure := od.currentPressure
	od.pressureMu.RUnlock()

	spreadScore := math.Min(float64(opp.SpreadBPS)/100.0, 1.0)

	pressureScore := math.Min(pressure.Hadamard, 1.0)

	coherenceScore := pressure.PauliX

	tensionScore := 1.0 - math.Abs(pressure.PauliZ-0.5)*2.0

	age := time.Since(opp.DetectedAt)
	maxAge := od.config.MaxOpportunityAge
	timingScore := 1.0 - float64(age)/float64(maxAge)
	if timingScore < 0 {
		timingScore = 0
	}

	baseScore := opp.Score

	opp.Score = baseScore +
		spreadScore*0.50 +
		pressureScore*0.10 +
		coherenceScore*0.08 +
		tensionScore*0.08 +
		timingScore*0.04

	opp.Pressure = pressure
}

func (od *OpportunityDetector) UpdatePressure(pressure PressureMetrics) {
	od.pressureMu.Lock()
	defer od.pressureMu.Unlock()
	od.currentPressure = pressure
}

func (od *OpportunityDetector) pruneExpired() {
	od.mu.Lock()
	defer od.mu.Unlock()

	now := time.Now()
	for id, opp := range od.opportunities {
		if now.After(opp.ExpiresAt) {
			opp.Status = StatusExpired
			delete(od.opportunities, id)
		}
	}
}

func (od *OpportunityDetector) removeLowestScored() {
	var lowestID [32]byte
	lowestScore := float64(2.0)

	for id, opp := range od.opportunities {
		if opp.Score < lowestScore {
			lowestScore = opp.Score
			lowestID = id
		}
	}

	if lowestScore < 2.0 {
		delete(od.opportunities, lowestID)
	}
}

func (od *OpportunityDetector) GetOpportunities() []*ArbitrageOpportunity {
	od.mu.RLock()
	defer od.mu.RUnlock()

	result := make([]*ArbitrageOpportunity, 0, len(od.opportunities))
	for _, opp := range od.opportunities {
		result = append(result, opp)
	}
	return result
}

func (od *OpportunityDetector) GetTopOpportunities(n int) []*ArbitrageOpportunity {
	opps := od.GetOpportunities()

	for i := 0; i < len(opps)-1; i++ {
		for j := i + 1; j < len(opps); j++ {
			if opps[j].Score > opps[i].Score {
				opps[i], opps[j] = opps[j], opps[i]
			}
		}
	}

	if n > len(opps) {
		n = len(opps)
	}
	return opps[:n]
}

func (od *OpportunityDetector) GetOpportunity(id [32]byte) *ArbitrageOpportunity {
	od.mu.RLock()
	defer od.mu.RUnlock()
	return od.opportunities[id]
}

func (od *OpportunityDetector) GetOpportunityCount() int {
	od.mu.RLock()
	defer od.mu.RUnlock()
	return len(od.opportunities)
}

func (od *OpportunityDetector) MarkExecuting(id [32]byte) bool {
	od.mu.Lock()
	defer od.mu.Unlock()

	opp, exists := od.opportunities[id]
	if !exists {
		return false
	}

	opp.Status = StatusExecuting
	return true
}

func (od *OpportunityDetector) MarkCompleted(id [32]byte, success bool) {
	od.mu.Lock()
	defer od.mu.Unlock()

	opp, exists := od.opportunities[id]
	if !exists {
		return
	}

	if success {
		opp.Status = StatusCompleted
	} else {
		opp.Status = StatusFailed
	}

	delete(od.opportunities, id)
}

func (od *OpportunityDetector) ConnectShotgunToTransport(transport *node.MultiChainTransport) {
	if od.shotgun != nil {
		od.shotgun.ConnectToTransport(transport)
	}
}

func (od *OpportunityDetector) GetShotgunStats() *ShotgunStats {
	if od.shotgun == nil {
		return nil
	}
	stats := od.shotgun.GetStats()
	return &stats
}
