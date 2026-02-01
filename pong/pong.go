package pong

import (
	"encoding/binary"
	"sync"
	"time"

	"zeam/quantum"

	"github.com/ethereum/go-ethereum/common"
)

const (
	FieldWidth  = 256
	FieldHeight = 128

	PaddleHeight = 20
	PaddleWidth  = 4
	PaddleMargin = 8

	BallSize = 4

	MaxBallSpeed  = 8
	PaddleSpeed   = 6
	InitialBallDX = 3
	InitialBallDY = 2

	WinScore = 11
)

type Input byte

const (
	InputNone Input = 0
	InputUp   Input = 1
	InputDown Input = 2
)

type State struct {
	BallX, BallY   int16
	BallDX, BallDY int16
	Paddle1Y       int16
	Paddle2Y       int16
	Score1, Score2 uint8
	Tick           uint64
	GameOver       bool
	Winner         uint8
}

func NewState() *State {
	return &State{
		BallX:    FieldWidth / 2,
		BallY:    FieldHeight / 2,
		BallDX:   InitialBallDX,
		BallDY:   InitialBallDY,
		Paddle1Y: (FieldHeight - PaddleHeight) / 2,
		Paddle2Y: (FieldHeight - PaddleHeight) / 2,
	}
}

func (s *State) Clone() *State {
	clone := *s
	return &clone
}

type Tick struct {
	State        *State
	Input1       Input
	Input2       Input
	Nonce        uint64
}

func (t *Tick) Encode() []byte {
	payload := make([]byte, 35)

	payload[0] = 0x5A
	payload[1] = 0x50
	payload[2] = 0x01

	binary.BigEndian.PutUint64(payload[3:11], t.State.Tick)

	binary.BigEndian.PutUint16(payload[11:13], uint16(t.State.BallX))
	binary.BigEndian.PutUint16(payload[13:15], uint16(t.State.BallY))
	binary.BigEndian.PutUint16(payload[15:17], uint16(t.State.BallDX+128))
	binary.BigEndian.PutUint16(payload[17:19], uint16(t.State.BallDY+128))

	binary.BigEndian.PutUint16(payload[19:21], uint16(t.State.Paddle1Y))
	binary.BigEndian.PutUint16(payload[21:23], uint16(t.State.Paddle2Y))

	payload[23] = t.State.Score1
	payload[24] = t.State.Score2
	payload[25] = byte(t.Input1)
	payload[26] = byte(t.Input2)

	binary.BigEndian.PutUint64(payload[27:35], t.Nonce)

	return payload
}

func ComputeNextState(current *State, input1, input2 Input, txHash common.Hash) *State {
	next := current.Clone()
	next.Tick++

	if next.GameOver {
		return next
	}

	h := txHash.Bytes()

	switch input1 {
	case InputUp:
		next.Paddle1Y -= PaddleSpeed
	case InputDown:
		next.Paddle1Y += PaddleSpeed
	}
	next.Paddle1Y += int16(h[0]%3) - 1

	switch input2 {
	case InputUp:
		next.Paddle2Y -= PaddleSpeed
	case InputDown:
		next.Paddle2Y += PaddleSpeed
	}
	next.Paddle2Y += int16(h[1]%3) - 1

	next.Paddle1Y = clamp(next.Paddle1Y, 0, FieldHeight-PaddleHeight)
	next.Paddle2Y = clamp(next.Paddle2Y, 0, FieldHeight-PaddleHeight)

	next.BallX += next.BallDX
	next.BallY += next.BallDY

	speedMod := int16(h[2]%5) - 2
	if next.BallDX > 0 {
		next.BallDX = clamp(next.BallDX+speedMod/2, 1, MaxBallSpeed)
	} else {
		next.BallDX = clamp(next.BallDX-speedMod/2, -MaxBallSpeed, -1)
	}

	if next.BallY <= 0 {
		next.BallY = -next.BallY
		next.BallDY = -next.BallDY + int16(h[4]%3) - 1
	}
	if next.BallY >= FieldHeight-BallSize {
		next.BallY = 2*(FieldHeight-BallSize) - next.BallY
		next.BallDY = -next.BallDY + int16(h[5]%3) - 1
	}

	if next.BallX <= PaddleMargin+PaddleWidth && next.BallX >= PaddleMargin-BallSize {
		if next.BallY+BallSize >= next.Paddle1Y && next.BallY <= next.Paddle1Y+PaddleHeight {
			next.BallX = PaddleMargin + PaddleWidth + 1
			next.BallDX = -next.BallDX
			hitPos := next.BallY - next.Paddle1Y + BallSize/2
			spin := int16((hitPos*6/PaddleHeight) - 3)
			next.BallDY = clamp(next.BallDY+spin+int16(h[6]%5)-2, -MaxBallSpeed, MaxBallSpeed)
			if next.BallDX < MaxBallSpeed {
				next.BallDX++
			}
		}
	}

	paddle2Left := int16(FieldWidth - PaddleMargin - PaddleWidth)
	if next.BallX+BallSize >= paddle2Left && next.BallX <= FieldWidth-PaddleMargin {
		if next.BallY+BallSize >= next.Paddle2Y && next.BallY <= next.Paddle2Y+PaddleHeight {
			next.BallX = paddle2Left - BallSize - 1
			next.BallDX = -next.BallDX
			hitPos := next.BallY - next.Paddle2Y + BallSize/2
			spin := int16((hitPos*6/PaddleHeight) - 3)
			next.BallDY = clamp(next.BallDY+spin+int16(h[7]%5)-2, -MaxBallSpeed, MaxBallSpeed)
			if next.BallDX > -MaxBallSpeed {
				next.BallDX--
			}
		}
	}

	if next.BallX < 0 {
		next.Score2++
		resetBall(next, h, 2)
	} else if next.BallX > FieldWidth {
		next.Score1++
		resetBall(next, h, 1)
	}

	if next.Score1 >= WinScore {
		next.GameOver, next.Winner = true, 1
	} else if next.Score2 >= WinScore {
		next.GameOver, next.Winner = true, 2
	}

	return next
}

func resetBall(s *State, h []byte, scoredAgainst uint8) {
	s.BallX = FieldWidth / 2
	s.BallY = FieldHeight / 2
	if scoredAgainst == 1 {
		s.BallDX = -InitialBallDX
	} else {
		s.BallDX = InitialBallDX
	}
	s.BallDY = int16(h[8]%5) - 2
	if s.BallDY == 0 {
		s.BallDY = 1
	}
}

func clamp(v, min, max int16) int16 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

type Engine struct {
	state   *State
	stateMu sync.RWMutex

	computeHashWithError func(payload []byte) (common.Hash, error)

	input1, input2 Input
	inputMu        sync.Mutex

	tickRate  time.Duration
	tickNonce uint64

	OnStateChange func(*State)
	OnScore       func(player, score1, score2 uint8)
	OnGameOver    func(winner uint8)
	OnL1Error     func(error)

	running bool
	stopCh  chan struct{}
}

func NewEngine(computeHash func([]byte) common.Hash) *Engine {
	return &Engine{
		state: NewState(),
		computeHashWithError: func(payload []byte) (common.Hash, error) {
			return computeHash(payload), nil
		},
		tickRate: 500 * time.Millisecond,
		stopCh:   make(chan struct{}),
	}
}

func NewEngineWithError(computeHash func([]byte) (common.Hash, error)) *Engine {
	return &Engine{
		state:                NewState(),
		computeHashWithError: computeHash,
		tickRate:             500 * time.Millisecond,
		stopCh:               make(chan struct{}),
	}
}

func (e *Engine) SetTickRate(d time.Duration) { e.tickRate = d }

func (e *Engine) State() *State {
	e.stateMu.RLock()
	defer e.stateMu.RUnlock()
	return e.state.Clone()
}

func (e *Engine) SetInput(player uint8, input Input) {
	e.inputMu.Lock()
	defer e.inputMu.Unlock()
	if player == 1 {
		e.input1 = input
	} else {
		e.input2 = input
	}
}

func (e *Engine) getAndClearInputs() (Input, Input) {
	e.inputMu.Lock()
	defer e.inputMu.Unlock()
	i1, i2 := e.input1, e.input2
	e.input1, e.input2 = InputNone, InputNone
	return i1, i2
}

func (e *Engine) Start() {
	if e.running {
		return
	}
	e.running = true
	go e.loop()
}

func (e *Engine) Stop() {
	if !e.running {
		return
	}
	e.running = false
	close(e.stopCh)
}

func (e *Engine) loop() {
	ticker := time.NewTicker(e.tickRate)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.tick()
		}
	}
}

func (e *Engine) tick() {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()

	if e.state.GameOver {
		return
	}

	input1, input2 := e.getAndClearInputs()

	payload := (&Tick{State: e.state, Input1: input1, Input2: input2, Nonce: e.tickNonce}).Encode()
	e.tickNonce++

	txHash, err := e.computeHashWithError(payload)
	if err != nil {

		if e.OnL1Error != nil {
			e.OnL1Error(err)
		}
		return
	}

	prevS1, prevS2 := e.state.Score1, e.state.Score2
	e.state = ComputeNextState(e.state, input1, input2, txHash)

	if e.OnStateChange != nil {
		e.OnStateChange(e.state.Clone())
	}
	if e.OnScore != nil && (e.state.Score1 != prevS1 || e.state.Score2 != prevS2) {
		if e.state.Score1 != prevS1 {
			e.OnScore(1, e.state.Score1, e.state.Score2)
		} else {
			e.OnScore(2, e.state.Score1, e.state.Score2)
		}
	}
	if e.OnGameOver != nil && e.state.GameOver {
		e.OnGameOver(e.state.Winner)
	}
}

func (e *Engine) Reset() {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	e.state = NewState()
	e.tickNonce = 0
}

type QuantumEngine struct {
	*Engine
	qs *quantum.QuantumService
}

func NewQuantumEngine(computeHash func([]byte) (common.Hash, error)) *QuantumEngine {
	qe := &QuantumEngine{
		Engine: NewEngineWithError(computeHash),
		qs:     quantum.GetService(),
	}

	qe.qs.OnPressureChange(func(p quantum.PressureMetrics) {

		baseRate := 500 * time.Millisecond
		pressureMod := time.Duration(float64(baseRate) * (1.0 - p.Hadamard*0.5))
		if pressureMod < 100*time.Millisecond {
			pressureMod = 100 * time.Millisecond
		}
		qe.Engine.tickRate = pressureMod
	})

	return qe
}

func ComputeNextStateQuantum(current *State, input1, input2 Input, txHash common.Hash, pressure quantum.PressureMetrics) *State {
	next := current.Clone()
	next.Tick++

	if next.GameOver {
		return next
	}

	h := txHash.Bytes()

	speedMod := int16(pressure.Hadamard * 3)
	spinMod := int16((pressure.PauliZ - 0.5) * 4)
	precision := pressure.PauliX

	paddleSpeed := int16(float64(PaddleSpeed) * (0.5 + precision*0.5))

	switch input1 {
	case InputUp:
		next.Paddle1Y -= paddleSpeed
	case InputDown:
		next.Paddle1Y += paddleSpeed
	}
	if precision < 0.5 {
		next.Paddle1Y += int16(h[0]%3) - 1
	}

	switch input2 {
	case InputUp:
		next.Paddle2Y -= paddleSpeed
	case InputDown:
		next.Paddle2Y += paddleSpeed
	}
	if precision < 0.5 {
		next.Paddle2Y += int16(h[1]%3) - 1
	}

	next.Paddle1Y = clamp(next.Paddle1Y, 0, FieldHeight-PaddleHeight)
	next.Paddle2Y = clamp(next.Paddle2Y, 0, FieldHeight-PaddleHeight)

	next.BallX += next.BallDX + speedMod*sign(next.BallDX)
	next.BallY += next.BallDY

	hashSpeedMod := int16(h[2]%5) - 2 + speedMod
	if next.BallDX > 0 {
		next.BallDX = clamp(next.BallDX+hashSpeedMod/2, 1, MaxBallSpeed+speedMod)
	} else {
		next.BallDX = clamp(next.BallDX-hashSpeedMod/2, -MaxBallSpeed-speedMod, -1)
	}

	if next.BallY <= 0 {
		next.BallY = -next.BallY
		next.BallDY = -next.BallDY + int16(h[4]%3) - 1 + spinMod
	}
	if next.BallY >= FieldHeight-BallSize {
		next.BallY = 2*(FieldHeight-BallSize) - next.BallY
		next.BallDY = -next.BallDY + int16(h[5]%3) - 1 + spinMod
	}

	if next.BallX <= PaddleMargin+PaddleWidth && next.BallX >= PaddleMargin-BallSize {
		if next.BallY+BallSize >= next.Paddle1Y && next.BallY <= next.Paddle1Y+PaddleHeight {
			next.BallX = PaddleMargin + PaddleWidth + 1
			next.BallDX = -next.BallDX
			hitPos := next.BallY - next.Paddle1Y + BallSize/2
			spin := int16((hitPos*6/PaddleHeight) - 3)
			next.BallDY = clamp(next.BallDY+spin+int16(h[6]%5)-2+spinMod, -MaxBallSpeed-speedMod, MaxBallSpeed+speedMod)
			if next.BallDX < MaxBallSpeed+speedMod {
				next.BallDX++
			}
		}
	}

	paddle2Left := int16(FieldWidth - PaddleMargin - PaddleWidth)
	if next.BallX+BallSize >= paddle2Left && next.BallX <= FieldWidth-PaddleMargin {
		if next.BallY+BallSize >= next.Paddle2Y && next.BallY <= next.Paddle2Y+PaddleHeight {
			next.BallX = paddle2Left - BallSize - 1
			next.BallDX = -next.BallDX
			hitPos := next.BallY - next.Paddle2Y + BallSize/2
			spin := int16((hitPos*6/PaddleHeight) - 3)
			next.BallDY = clamp(next.BallDY+spin+int16(h[7]%5)-2+spinMod, -MaxBallSpeed-speedMod, MaxBallSpeed+speedMod)
			if next.BallDX > -MaxBallSpeed-speedMod {
				next.BallDX--
			}
		}
	}

	if next.BallX < 0 {
		next.Score2++
		resetBall(next, h, 2)
	} else if next.BallX > FieldWidth {
		next.Score1++
		resetBall(next, h, 1)
	}

	if next.Score1 >= WinScore {
		next.GameOver, next.Winner = true, 1
	} else if next.Score2 >= WinScore {
		next.GameOver, next.Winner = true, 2
	}

	return next
}

func sign(x int16) int16 {
	if x > 0 {
		return 1
	} else if x < 0 {
		return -1
	}
	return 0
}

func (qe *QuantumEngine) GetQuantumState() (*State, quantum.PressureMetrics) {
	return qe.State(), qe.qs.GetPressure()
}

func (qe *QuantumEngine) TickQuantum() {
	qe.Engine.stateMu.Lock()
	defer qe.Engine.stateMu.Unlock()

	if qe.Engine.state.GameOver {
		return
	}

	input1, input2 := qe.Engine.getAndClearInputs()

	payload := (&Tick{State: qe.Engine.state, Input1: input1, Input2: input2, Nonce: qe.Engine.tickNonce}).Encode()
	qe.Engine.tickNonce++

	txHash, err := qe.Engine.computeHashWithError(payload)
	if err != nil {
		if qe.Engine.OnL1Error != nil {
			qe.Engine.OnL1Error(err)
		}
		return
	}

	pressure := qe.qs.GetPressure()

	qe.qs.OnHashReceived(quantum.NetworkLocal, txHash.Bytes())

	prevS1, prevS2 := qe.Engine.state.Score1, qe.Engine.state.Score2
	qe.Engine.state = ComputeNextStateQuantum(qe.Engine.state, input1, input2, txHash, pressure)

	if qe.Engine.OnStateChange != nil {
		qe.Engine.OnStateChange(qe.Engine.state.Clone())
	}
	if qe.Engine.OnScore != nil && (qe.Engine.state.Score1 != prevS1 || qe.Engine.state.Score2 != prevS2) {

		qe.qs.AddPressure("pong_score", 0, 0.5)
		if qe.Engine.state.Score1 != prevS1 {
			qe.Engine.OnScore(1, qe.Engine.state.Score1, qe.Engine.state.Score2)
		} else {
			qe.Engine.OnScore(2, qe.Engine.state.Score1, qe.Engine.state.Score2)
		}
	}
	if qe.Engine.OnGameOver != nil && qe.Engine.state.GameOver {
		qe.Engine.OnGameOver(qe.Engine.state.Winner)
	}
}
