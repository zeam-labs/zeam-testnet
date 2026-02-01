package arb

import (
	"log"
	"math/big"
	"sync"

	"zeam/node"
	"zeam/quantum"
)

type FlowOrchestrator struct {
	mu sync.Mutex

	engine     *FlowPressureEngine
	planner    *ExecutionPlanner
	strategies []Strategy

	OnProfit func(profit *big.Int)

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

type FlowOrchestratorConfig struct {
	EngineConfig  *FlowEngineConfig
	PlannerConfig *ExecutionPlannerConfig

	SignalBufferSize int
}

func DefaultFlowOrchestratorConfig() *FlowOrchestratorConfig {
	return &FlowOrchestratorConfig{
		EngineConfig:    DefaultFlowEngineConfig(),
		PlannerConfig:   DefaultExecutionPlannerConfig(),
		SignalBufferSize: 64,
	}
}

func NewFlowOrchestrator(
	feed node.NetworkFeed,
	parser *MempoolParser,
	qs *quantum.QuantumService,
	queue *OpportunityQueue,
	flashblocks *FlashblocksClient,
	config *FlowOrchestratorConfig,
) *FlowOrchestrator {
	if config == nil {
		config = DefaultFlowOrchestratorConfig()
	}

	engine := NewFlowPressureEngine(feed, parser, qs, config.EngineConfig)
	planner := NewExecutionPlanner(queue, flashblocks, config.PlannerConfig)

	return &FlowOrchestrator{
		engine:  engine,
		planner: planner,
		stopCh:  make(chan struct{}),
	}
}

func (fo *FlowOrchestrator) RegisterStrategy(s Strategy) {
	fo.mu.Lock()
	defer fo.mu.Unlock()
	fo.strategies = append(fo.strategies, s)
}

func (fo *FlowOrchestrator) Start() error {
	fo.mu.Lock()
	if fo.running {
		fo.mu.Unlock()
		return nil
	}
	fo.running = true
	fo.mu.Unlock()

	if err := fo.engine.Start(); err != nil {
		return err
	}

	bufSize := 64
	if cfg := DefaultFlowOrchestratorConfig(); cfg.SignalBufferSize > 0 {
		bufSize = cfg.SignalBufferSize
	}

	for _, s := range fo.strategies {
		ch := fo.engine.Subscribe(bufSize)
		fo.wg.Add(1)
		go fo.runStrategy(s, ch)
	}

	log.Printf("[orchestrator] started with %d strategies", len(fo.strategies))
	return nil
}

func (fo *FlowOrchestrator) Stop() {
	fo.mu.Lock()
	if !fo.running {
		fo.mu.Unlock()
		return
	}
	fo.running = false
	fo.mu.Unlock()

	close(fo.stopCh)
	fo.engine.Stop()
	fo.wg.Wait()

	log.Printf("[orchestrator] stopped")
}

func (fo *FlowOrchestrator) Engine() *FlowPressureEngine {
	return fo.engine
}

func (fo *FlowOrchestrator) Planner() *ExecutionPlanner {
	return fo.planner
}

func (fo *FlowOrchestrator) runStrategy(s Strategy, signals <-chan FlowSignal) {
	defer fo.wg.Done()

	eventCount := 0

	for {
		select {
		case <-fo.stopCh:
			return
		case signal, ok := <-signals:
			if !ok {
				return
			}
			state := fo.engine.GetState()
			intent := s.OnSignal(signal, state)
			if intent != nil {
				fo.planner.SubmitIntent(intent)
			}

			eventCount++
			if eventCount >= 100 {
				fo.planner.PruneExpired()
				eventCount = 0
			}
		}
	}
}

func (fo *FlowOrchestrator) RecordProfit(profit *big.Int) {
	if profit == nil || profit.Sign() <= 0 {
		return
	}
	if fo.OnProfit != nil {
		fo.OnProfit(profit)
	}
}
