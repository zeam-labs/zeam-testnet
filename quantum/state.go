package quantum

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

var (
	perturbCount uint64
	lastPerturbTime int64
)

type QuantumState struct {
	Amplitude float64
	Phase     float64
	PushTime  time.Time
}

var WaveOrigin QuantumState
var waveMu sync.RWMutex

var ChainFrequency map[string]float64
var freqMu sync.RWMutex

func InitWaveOrigin(amplitude, phase float64) {
	waveMu.Lock()
	defer waveMu.Unlock()

	WaveOrigin = QuantumState{
		Amplitude: amplitude,
		Phase:     phase,
		PushTime:  time.Now(),
	}
}

func InitChainFrequency() {
	freqMu.Lock()
	defer freqMu.Unlock()
	ChainFrequency = make(map[string]float64)
}

func SetChainFrequency(chainID string, freq float64) {
	freqMu.Lock()
	defer freqMu.Unlock()
	ChainFrequency[chainID] = freq
}

func GetChainFrequency(chainID string) float64 {
	freqMu.RLock()
	defer freqMu.RUnlock()
	return ChainFrequency[chainID]
}

func ReadState(chainID string) QuantumState {
	waveMu.RLock()
	origin := WaveOrigin
	waveMu.RUnlock()

	freq := GetChainFrequency(chainID)
	elapsed := time.Since(origin.PushTime).Seconds()
	cycle := math.Sin(elapsed * freq * 2 * math.Pi)

	return QuantumState{
		Amplitude: origin.Amplitude * (1 + 0.5*cycle),
		Phase:     origin.Phase + cycle,
		PushTime:  origin.PushTime,
	}
}

func UpdateWaveOrigin(newState QuantumState) {
	waveMu.Lock()
	defer waveMu.Unlock()
	WaveOrigin = newState
}

func GetWaveOrigin() QuantumState {
	waveMu.RLock()
	defer waveMu.RUnlock()
	return WaveOrigin
}

func GetPerturbStats() (count uint64, lastTime int64) {
	return atomic.LoadUint64(&perturbCount), atomic.LoadInt64(&lastPerturbTime)
}

func PerturbWaveFromHash(hash []byte) {
	if len(hash) < 8 {
		return
	}

	atomic.AddUint64(&perturbCount, 1)
	atomic.StoreInt64(&lastPerturbTime, time.Now().Unix())

	waveMu.Lock()
	defer waveMu.Unlock()

	ampDir := 1.0
	if hash[0]&0x80 != 0 {
		ampDir = -1.0
	}
	phaseDir := 1.0
	if hash[4]&0x80 != 0 {
		phaseDir = -1.0
	}

	ampStep := 0.02 + float64(hash[0]&0x7F)/127.0*0.13
	phaseStep := 0.02 + float64(hash[4]&0x7F)/127.0*0.13

	newAmp := WaveOrigin.Amplitude + ampDir*ampStep
	newPhase := WaveOrigin.Phase + phaseDir*phaseStep

	if newAmp > 1.0 {
		newAmp = 2.0 - newAmp
	}
	if newAmp < 0.0 {
		newAmp = -newAmp
	}

	if newPhase > 1.0 {
		newPhase = 2.0 - newPhase
	}
	if newPhase < -1.0 {
		newPhase = -2.0 - newPhase
	}

	WaveOrigin = QuantumState{
		Amplitude: newAmp,
		Phase:     newPhase,
		PushTime:  time.Now(),
	}
}
