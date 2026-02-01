package pong

import (
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type FlowProvider interface {

	NextBytes(n int) []byte

	Stats() (hashCount uint64, flowRate float64)
}

type Adversary struct {
	mu sync.Mutex

	flow FlowProvider

	reactionSpeed  float64
	imprecision    float64
	aggressiveness float64

	targetY      int16
	lastFlowByte byte
}

func NewAdversary(flow FlowProvider) *Adversary {
	return &Adversary{
		flow:           flow,
		reactionSpeed:  0.6,
		imprecision:    0.3,
		aggressiveness: 0.5,
	}
}

func (a *Adversary) SetDifficulty(difficulty float64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if difficulty < 0 {
		difficulty = 0
	}
	if difficulty > 1 {
		difficulty = 1
	}

	a.reactionSpeed = 0.3 + difficulty*0.6
	a.imprecision = 0.5 - difficulty*0.4
	a.aggressiveness = 0.3 + difficulty*0.5
}

func (a *Adversary) Decide(state *State) Input {
	a.mu.Lock()
	defer a.mu.Unlock()

	if state.GameOver {
		return InputNone
	}

	var flowEntropy byte
	var flowMod float64 = 0.5

	if a.flow != nil {
		bytes := a.flow.NextBytes(1)
		if len(bytes) > 0 {
			flowEntropy = bytes[0]
			a.lastFlowByte = flowEntropy
		}

		_, flowRate := a.flow.Stats()
		if flowRate > 0 {

			flowMod = 0.5 + minFloat(flowRate/100.0, 1.0)*0.5
		}
	}

	a.targetY = a.calculateTarget(state, flowEntropy)

	paddleCenter := state.Paddle2Y + PaddleHeight/2

	tolerance := int16(float64(PaddleHeight) * a.imprecision * (float64(flowEntropy%64) / 64.0))
	if tolerance < 2 {
		tolerance = 2
	}

	diff := a.targetY - paddleCenter

	effectiveSpeed := a.reactionSpeed * flowMod
	if abs16(diff) < tolerance {

		if flowEntropy%4 == 0 {
			return InputNone
		}
	}

	if float64(flowEntropy%100)/100.0 > effectiveSpeed {

		return InputNone
	}

	if diff > tolerance {
		return InputDown
	} else if diff < -tolerance {
		return InputUp
	}

	return InputNone
}

func (a *Adversary) calculateTarget(state *State, flowEntropy byte) int16 {
	ballX := state.BallX
	ballY := state.BallY
	ballDX := state.BallDX
	ballDY := state.BallDY

	if ballDX <= 0 {

		centerY := int16(FieldHeight/2) + int16(flowEntropy%20) - 10
		return centerY
	}

	paddle2X := int16(FieldWidth - PaddleMargin - PaddleWidth)
	distanceToTravel := paddle2X - ballX

	if ballDX == 0 {
		return ballY
	}

	ticksToReach := distanceToTravel / ballDX
	if ticksToReach < 0 {
		ticksToReach = 0
	}

	predictedY := ballY + ballDY*int16(float64(ticksToReach)*a.aggressiveness)

	for predictedY < 0 || predictedY > FieldHeight-BallSize {
		if predictedY < 0 {
			predictedY = -predictedY
		}
		if predictedY > FieldHeight-BallSize {
			predictedY = 2*(FieldHeight-BallSize) - predictedY
		}
	}

	imprecisionAmount := int16(float64(PaddleHeight) * a.imprecision)
	flowOffset := int16(flowEntropy%uint8(imprecisionAmount*2+1)) - imprecisionAmount
	predictedY += flowOffset

	if predictedY < PaddleHeight/2 {
		predictedY = PaddleHeight / 2
	}
	if predictedY > FieldHeight-PaddleHeight/2 {
		predictedY = FieldHeight - PaddleHeight/2
	}

	return predictedY
}

func abs16(x int16) int16 {
	if x < 0 {
		return -x
	}
	return x
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

type AdversaryEngine struct {
	*Engine
	adversary *Adversary
}

func NewAdversaryEngine(computeHash func([]byte) (common.Hash, error), flow FlowProvider) *AdversaryEngine {
	ae := &AdversaryEngine{
		Engine:    NewEngineWithError(computeHash),
		adversary: NewAdversary(flow),
	}
	return ae
}

func (ae *AdversaryEngine) SetAdversaryDifficulty(difficulty float64) {
	ae.adversary.SetDifficulty(difficulty)
}

func (ae *AdversaryEngine) Start() {
	if ae.Engine.running {
		return
	}
	ae.Engine.running = true
	go ae.loopWithAdversary()
}

func (ae *AdversaryEngine) loopWithAdversary() {
	ticker := time.NewTicker(ae.Engine.tickRate)
	defer ticker.Stop()

	for {
		select {
		case <-ae.Engine.stopCh:
			return
		case <-ticker.C:
			ae.tickWithAdversary()
		}
	}
}

func (ae *AdversaryEngine) tickWithAdversary() {
	ae.Engine.stateMu.Lock()
	defer ae.Engine.stateMu.Unlock()

	if ae.Engine.state.GameOver {
		return
	}

	ae.Engine.inputMu.Lock()
	input1 := ae.Engine.input1
	ae.Engine.input1 = InputNone
	ae.Engine.inputMu.Unlock()

	input2 := ae.adversary.Decide(ae.Engine.state)

	payload := (&Tick{State: ae.Engine.state, Input1: input1, Input2: input2, Nonce: ae.Engine.tickNonce}).Encode()
	ae.Engine.tickNonce++

	txHash, err := ae.Engine.computeHashWithError(payload)
	if err != nil {
		if ae.Engine.OnL1Error != nil {
			ae.Engine.OnL1Error(err)
		}
		return
	}

	prevS1, prevS2 := ae.Engine.state.Score1, ae.Engine.state.Score2
	ae.Engine.state = ComputeNextState(ae.Engine.state, input1, input2, txHash)

	if ae.Engine.OnStateChange != nil {
		ae.Engine.OnStateChange(ae.Engine.state.Clone())
	}
	if ae.Engine.OnScore != nil && (ae.Engine.state.Score1 != prevS1 || ae.Engine.state.Score2 != prevS2) {
		if ae.Engine.state.Score1 != prevS1 {
			ae.Engine.OnScore(1, ae.Engine.state.Score1, ae.Engine.state.Score2)
		} else {
			ae.Engine.OnScore(2, ae.Engine.state.Score1, ae.Engine.state.Score2)
		}
	}
	if ae.Engine.OnGameOver != nil && ae.Engine.state.GameOver {
		ae.Engine.OnGameOver(ae.Engine.state.Winner)
	}
}
