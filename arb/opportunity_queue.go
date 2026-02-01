package arb

import (
	"container/heap"
	"log"
	"sync"
	"time"
)

type OpportunityQueue struct {
	mu sync.Mutex

	items opportunityHeap

	byID map[[32]byte]*QueuedOpportunity

	executing map[[32]byte]time.Time
	completed map[[32]byte]time.Time

	maxSize           int
	maxConcurrent     int
	executionTimeout  time.Duration
	cooldownPeriod    time.Duration

	OnReady func(opp *ScoredOpportunity)

	enqueued   uint64
	dequeued   uint64
	expired    uint64
	duplicates uint64
}

type QueuedOpportunity struct {
	*ScoredOpportunity
	index    int
	queuedAt time.Time
}

type opportunityHeap []*QueuedOpportunity

func (h opportunityHeap) Len() int { return len(h) }

func (h opportunityHeap) Less(i, j int) bool {

	if h[i].Priority != h[j].Priority {
		return h[i].Priority > h[j].Priority
	}

	return h[i].DetectedAt.Before(h[j].DetectedAt)
}

func (h opportunityHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *opportunityHeap) Push(x interface{}) {
	n := len(*h)
	item := x.(*QueuedOpportunity)
	item.index = n
	*h = append(*h, item)
}

func (h *opportunityHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[0 : n-1]
	return item
}

type OpportunityQueueConfig struct {
	MaxSize          int
	MaxConcurrent    int
	ExecutionTimeout time.Duration
	CooldownPeriod   time.Duration
}

func DefaultOpportunityQueueConfig() *OpportunityQueueConfig {
	return &OpportunityQueueConfig{
		MaxSize:          100,
		MaxConcurrent:    3,
		ExecutionTimeout: 30 * time.Second,
		CooldownPeriod:   5 * time.Second,
	}
}

func NewOpportunityQueue(config *OpportunityQueueConfig) *OpportunityQueue {
	if config == nil {
		config = DefaultOpportunityQueueConfig()
	}

	q := &OpportunityQueue{
		items:            make(opportunityHeap, 0),
		byID:             make(map[[32]byte]*QueuedOpportunity),
		executing:        make(map[[32]byte]time.Time),
		completed:        make(map[[32]byte]time.Time),
		maxSize:          config.MaxSize,
		maxConcurrent:    config.MaxConcurrent,
		executionTimeout: config.ExecutionTimeout,
		cooldownPeriod:   config.CooldownPeriod,
	}

	heap.Init(&q.items)

	return q
}

func (q *OpportunityQueue) Enqueue(opp *ScoredOpportunity) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, exists := q.byID[opp.ID]; exists {
		q.duplicates++
		return false
	}

	if completedAt, ok := q.completed[opp.ID]; ok {
		if time.Since(completedAt) < q.cooldownPeriod {
			return false
		}
		delete(q.completed, opp.ID)
	}

	if len(q.items) >= q.maxSize {

		if len(q.items) > 0 {
			lowest := q.items[len(q.items)-1]
			if opp.Priority <= lowest.Priority {
				return false
			}

			heap.Remove(&q.items, lowest.index)
			delete(q.byID, lowest.ID)
			q.expired++
		}
	}

	queued := &QueuedOpportunity{
		ScoredOpportunity: opp,
		queuedAt:          time.Now(),
	}
	heap.Push(&q.items, queued)
	q.byID[opp.ID] = queued
	q.enqueued++

	log.Printf("[QUEUE] Enqueued opportunity: priority=%d, spread=%dbps, score=%.3f",
		opp.Priority, opp.SpreadBPS, opp.Score)

	return true
}

func (q *OpportunityQueue) Dequeue() *ScoredOpportunity {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.executing) >= q.maxConcurrent {
		return nil
	}

	q.removeExpired()

	if len(q.items) == 0 {
		return nil
	}

	queued := heap.Pop(&q.items).(*QueuedOpportunity)
	delete(q.byID, queued.ID)
	q.dequeued++

	q.executing[queued.ID] = time.Now()

	return queued.ScoredOpportunity
}

func (q *OpportunityQueue) Peek() *ScoredOpportunity {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return nil
	}

	return q.items[0].ScoredOpportunity
}

func (q *OpportunityQueue) MarkComplete(id [32]byte, success bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.executing, id)
	q.completed[id] = time.Now()

	if success {
		log.Printf("[QUEUE] Opportunity %x completed successfully", id[:4])
	} else {
		log.Printf("[QUEUE] Opportunity %x failed", id[:4])
	}
}

func (q *OpportunityQueue) removeExpired() {
	now := time.Now()

	for id, startTime := range q.executing {
		if now.Sub(startTime) > q.executionTimeout {
			delete(q.executing, id)
			q.completed[id] = now
			log.Printf("[QUEUE] Execution timed out: %x", id[:4])
		}
	}

	toRemove := make([]int, 0)
	for i, item := range q.items {
		if now.After(item.ExpiresAt) {
			toRemove = append(toRemove, i)
		}
	}

	for i := len(toRemove) - 1; i >= 0; i-- {
		idx := toRemove[i]
		item := q.items[idx]
		heap.Remove(&q.items, idx)
		delete(q.byID, item.ID)
		q.expired++
	}
}

func (q *OpportunityQueue) CleanCompleted(maxAge time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, completedAt := range q.completed {
		if completedAt.Before(cutoff) {
			delete(q.completed, id)
		}
	}
}

func (q *OpportunityQueue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func (q *OpportunityQueue) ExecutingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.executing)
}

func (q *OpportunityQueue) CanExecute() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.executing) < q.maxConcurrent && len(q.items) > 0
}

func (q *OpportunityQueue) Stats() map[string]interface{} {
	q.mu.Lock()
	defer q.mu.Unlock()

	topPriorities := make([]int, 0, 5)
	for i := 0; i < len(q.items) && i < 5; i++ {
		topPriorities = append(topPriorities, q.items[i].Priority)
	}

	return map[string]interface{}{
		"size":           len(q.items),
		"executing":      len(q.executing),
		"max_concurrent": q.maxConcurrent,
		"enqueued":       q.enqueued,
		"dequeued":       q.dequeued,
		"expired":        q.expired,
		"duplicates":     q.duplicates,
		"top_priorities": topPriorities,
	}
}

func (q *OpportunityQueue) GetAll() []*ScoredOpportunity {
	q.mu.Lock()
	defer q.mu.Unlock()

	result := make([]*ScoredOpportunity, len(q.items))
	for i, item := range q.items {
		result[i] = item.ScoredOpportunity
	}

	return result
}

func (q *OpportunityQueue) UpdatePriority(id [32]byte, newPriority int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	item, exists := q.byID[id]
	if !exists {
		return false
	}

	item.Priority = newPriority
	heap.Fix(&q.items, item.index)

	return true
}

func (q *OpportunityQueue) Remove(id [32]byte) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	item, exists := q.byID[id]
	if !exists {
		return false
	}

	heap.Remove(&q.items, item.index)
	delete(q.byID, id)

	return true
}
