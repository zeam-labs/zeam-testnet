package arb

import (
	"sync"
	"time"
)

type ExecutionPlanner struct {
	mu sync.Mutex

	queue      *OpportunityQueue
	flashblocks *FlashblocksClient

	pendingValidation map[[32]byte]*ExecutionIntent

	recentIDs map[[32]byte]time.Time

	config *ExecutionPlannerConfig
}

type ExecutionPlannerConfig struct {

	FlashblockTimeout time.Duration

	DedupWindow time.Duration

	MaxPendingValidation int
}

func DefaultExecutionPlannerConfig() *ExecutionPlannerConfig {
	return &ExecutionPlannerConfig{
		FlashblockTimeout:    500 * time.Millisecond,
		DedupWindow:          10 * time.Second,
		MaxPendingValidation: 50,
	}
}

func NewExecutionPlanner(
	queue *OpportunityQueue,
	flashblocks *FlashblocksClient,
	config *ExecutionPlannerConfig,
) *ExecutionPlanner {
	if config == nil {
		config = DefaultExecutionPlannerConfig()
	}
	return &ExecutionPlanner{
		queue:             queue,
		flashblocks:       flashblocks,
		pendingValidation: make(map[[32]byte]*ExecutionIntent),
		recentIDs:         make(map[[32]byte]time.Time),
		config:            config,
	}
}

func (ep *ExecutionPlanner) SubmitIntent(intent *ExecutionIntent) bool {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	if t, ok := ep.recentIDs[intent.ID]; ok {
		if time.Since(t) < ep.config.DedupWindow {
			return false
		}
	}

	now := time.Now()
	if intent.IsExpired(now, 0) {
		return false
	}

	if intent.NeedsFlashblockValidation && ep.flashblocks != nil {
		if len(ep.pendingValidation) < ep.config.MaxPendingValidation {
			ep.pendingValidation[intent.ID] = intent
			return true
		}

	}

	return ep.enqueueIntent(intent)
}

func (ep *ExecutionPlanner) OnFlashblockDecision(decision *FlashblockDecision) {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	intent, ok := ep.pendingValidation[decision.IntentID]
	if !ok {
		return
	}
	delete(ep.pendingValidation, decision.IntentID)

	switch decision.Action {
	case FlashblockConfirm:
		ep.enqueueIntent(intent)
	case FlashblockModify:
		if decision.ModifiedIntent != nil {
			ep.enqueueIntent(decision.ModifiedIntent)
		}
	case FlashblockCancel:

	}
}

func (ep *ExecutionPlanner) PruneExpired() {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	now := time.Now()

	for id, intent := range ep.pendingValidation {
		if intent.IsExpired(now, 0) {
			delete(ep.pendingValidation, id)
			continue
		}

		if now.Sub(intent.EarliestTime) > ep.config.FlashblockTimeout {
			ep.enqueueIntent(intent)
			delete(ep.pendingValidation, id)
		}
	}

	for id, t := range ep.recentIDs {
		if now.Sub(t) > ep.config.DedupWindow {
			delete(ep.recentIDs, id)
		}
	}
}

func (ep *ExecutionPlanner) enqueueIntent(intent *ExecutionIntent) bool {
	if intent.Opportunity == nil {
		return false
	}

	scored := &ScoredOpportunity{
		ArbitrageOpportunity: intent.Opportunity,
		PressureScore:        intent.FlowConfidence,
		CoherenceScore:       intent.PressureAtEmit.DCoherenceDt,
		TimingScore:          intent.FlowConfidence,
	}

	ok := ep.queue.Enqueue(scored)
	if ok {
		ep.recentIDs[intent.ID] = time.Now()
	}
	return ok
}

func (ep *ExecutionPlanner) PendingCount() int {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	return len(ep.pendingValidation)
}
