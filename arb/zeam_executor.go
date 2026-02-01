package arb

import (
	"context"
	"crypto/ecdsa"
	"log"
	"sync"
	"time"

	"zeam/node"
)

type ZEAMExecutor struct {
	mu sync.RWMutex

	executor *Executor

	detector *ZEAMDetector
	queue    *OpportunityQueue

	transport *node.MultiChainTransport

	engine *FlowPressureEngine

	minFlowRate     float64
	maxWaitTime     time.Duration
	checkInterval   time.Duration

	scoredOpps     uint64
	timingDelayed  uint64
	timingExecuted uint64
	avgScoreVsProfit float64

	results chan *ZEAMResult

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

type ZEAMResult struct {
	*ExecutionResult

	DetectionScore  float64
	FlowRateAtExec  float64
	PressureAtExec  float64
	TimingDelay     time.Duration
	QueuePosition   int

	ScoreAccuracy float64
}

type ZEAMExecutorConfig struct {

	MinFlowRate   float64
	MaxWaitTime   time.Duration
	CheckInterval time.Duration

	QueueConfig *OpportunityQueueConfig

	ExecutorConfig *ExecutorConfig
}

func DefaultZEAMExecutorConfig() *ZEAMExecutorConfig {
	return &ZEAMExecutorConfig{
		MinFlowRate:    5.0,
		MaxWaitTime:    5 * time.Second,
		CheckInterval:  1 * time.Second,
		QueueConfig:    DefaultOpportunityQueueConfig(),
		ExecutorConfig: DefaultExecutorConfig(),
	}
}

func NewZEAMExecutor(
	transport *node.MultiChainTransport,
	positions *PositionManager,
	detector *ZEAMDetector,
	privateKey *ecdsa.PrivateKey,
	config *ZEAMExecutorConfig,
) *ZEAMExecutor {
	if config == nil {
		config = DefaultZEAMExecutorConfig()
	}

	baseExecutor := NewExecutor(transport, positions, nil, privateKey, config.ExecutorConfig)

	return &ZEAMExecutor{
		executor:      baseExecutor,
		detector:      detector,
		queue:         NewOpportunityQueue(config.QueueConfig),
		transport:     transport,
		minFlowRate:   config.MinFlowRate,
		maxWaitTime:   config.MaxWaitTime,
		checkInterval: config.CheckInterval,
		results:       make(chan *ZEAMResult, 100),
		stopCh:        make(chan struct{}),
	}
}

func (ze *ZEAMExecutor) Start() error {
	ze.mu.Lock()
	if ze.running {
		ze.mu.Unlock()
		return nil
	}
	ze.running = true
	ze.mu.Unlock()

	if err := ze.executor.Start(); err != nil {
		return err
	}

	if ze.detector != nil {
		ze.detector.OnOpportunity = ze.handleOpportunity
	}

	ze.wg.Add(1)
	go ze.executionLoop()

	ze.wg.Add(1)
	go ze.resultProcessor()

	log.Println("[ZEAM-EXEC] Started ZEAM-integrated executor")
	return nil
}

func (ze *ZEAMExecutor) Stop() {
	ze.mu.Lock()
	if !ze.running {
		ze.mu.Unlock()
		return
	}
	ze.running = false
	close(ze.stopCh)
	ze.mu.Unlock()

	ze.executor.Stop()
	ze.wg.Wait()
	close(ze.results)

	log.Println("[ZEAM-EXEC] Stopped")
}

func (ze *ZEAMExecutor) handleOpportunity(opp *ScoredOpportunity) {
	ze.mu.Lock()
	ze.scoredOpps++
	ze.mu.Unlock()

	if ze.queue.Enqueue(opp) {
		log.Printf("[ZEAM-EXEC] Queued opportunity: score=%.3f, priority=%d, spread=%dbps",
			opp.Score, opp.Priority, opp.SpreadBPS)
	}
}

func (ze *ZEAMExecutor) executionLoop() {
	defer ze.wg.Done()

	ticker := time.NewTicker(ze.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ze.stopCh:
			return
		case <-ticker.C:
			ze.tryExecute()
		}
	}
}

func (ze *ZEAMExecutor) tryExecute() {

	if !ze.queue.CanExecute() {
		return
	}

	top := ze.queue.Peek()
	if top == nil {
		return
	}

	var flowRate float64
	var shouldWait bool
	timeToExpiry := time.Until(top.ExpiresAt)

	if ze.engine != nil {

		gradient := ze.engine.GetPressureGradient()
		flowRate = ze.engine.GetState().FlowRate
		shouldWait = !gradient.IsAccelerating() && timeToExpiry > ze.checkInterval*2
	} else {

		flowRate = ze.transport.GetFlowRate()
		shouldWait = flowRate < ze.minFlowRate && timeToExpiry > ze.checkInterval*2
	}

	if shouldWait {
		ze.mu.Lock()
		ze.timingDelayed++
		ze.mu.Unlock()
		return
	}

	opp := ze.queue.Dequeue()
	if opp == nil {
		return
	}

	startTime := time.Now()
	timingDelay := startTime.Sub(opp.DetectedAt)

	var pressureAtExec float64
	if ze.detector != nil {
		stats := ze.detector.Stats()
		if p, ok := stats["total_pressure"].(float64); ok {
			pressureAtExec = p
		}
	}

	if err := ze.executor.Submit(opp.ArbitrageOpportunity); err != nil {
		log.Printf("[ZEAM-EXEC] Failed to submit: %v", err)
		ze.queue.MarkComplete(opp.ID, false)
		return
	}

	ze.mu.Lock()
	ze.timingExecuted++
	ze.mu.Unlock()

	log.Printf("[ZEAM-EXEC] EXECUTING: score=%.3f, flow=%.1f tx/s, delay=%v",
		opp.Score, flowRate, timingDelay)

	_ = ZEAMResult{
		DetectionScore: opp.Score,
		FlowRateAtExec: flowRate,
		PressureAtExec: pressureAtExec,
		TimingDelay:    timingDelay,
	}
}

func (ze *ZEAMExecutor) resultProcessor() {
	defer ze.wg.Done()

	for {
		select {
		case <-ze.stopCh:
			return
		case result, ok := <-ze.executor.Results():
			if !ok {
				return
			}

			zeamResult := &ZEAMResult{
				ExecutionResult: result,
			}

			ze.queue.MarkComplete(result.Opportunity.ID, result.Success)

			select {
			case ze.results <- zeamResult:
			default:

			}

			if result.Success {
				log.Printf("[ZEAM-EXEC] SUCCESS: latency=%v", result.Latency)
			} else {
				log.Printf("[ZEAM-EXEC] FAILED: %v", result.Error)
			}
		}
	}
}

func (ze *ZEAMExecutor) Results() <-chan *ZEAMResult {
	return ze.results
}

func (ze *ZEAMExecutor) SetNonce(chainID uint64, nonce uint64) {
	ze.executor.SetNonce(chainID, nonce)
}

func (ze *ZEAMExecutor) Stats() map[string]interface{} {
	ze.mu.RLock()
	defer ze.mu.RUnlock()

	baseStats := ze.executor.GetStats()

	return map[string]interface{}{
		"running":          ze.running,
		"scored_opps":      ze.scoredOpps,
		"timing_delayed":   ze.timingDelayed,
		"timing_executed":  ze.timingExecuted,
		"queue_size":       ze.queue.Size(),
		"queue_executing":  ze.queue.ExecutingCount(),
		"flow_rate":        ze.transport.GetFlowRate(),
		"base_stats":       baseStats,
		"queue_stats":      ze.queue.Stats(),
	}
}

func (ze *ZEAMExecutor) WaitForTiming(ctx context.Context) bool {
	deadline := time.Now().Add(ze.maxWaitTime)
	ticker := time.NewTicker(ze.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if time.Now().After(deadline) {
				return false
			}

			flowRate := ze.transport.GetFlowRate()
			if flowRate >= ze.minFlowRate {
				return true
			}
		}
	}
}

func (ze *ZEAMExecutor) ForceExecute(opp *ScoredOpportunity) error {
	return ze.executor.Submit(opp.ArbitrageOpportunity)
}

func (ze *ZEAMExecutor) GetQueue() *OpportunityQueue {
	return ze.queue
}

func (ze *ZEAMExecutor) GetDetector() *ZEAMDetector {
	return ze.detector
}

func (ze *ZEAMExecutor) SetFlowEngine(engine *FlowPressureEngine) {
	ze.mu.Lock()
	defer ze.mu.Unlock()
	ze.engine = engine
}
