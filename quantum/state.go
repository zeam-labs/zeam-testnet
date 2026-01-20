package quantum

import (
	"math"
	"sync"
	"time"
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
