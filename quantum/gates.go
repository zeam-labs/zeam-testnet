package quantum

import (
	"log"
	"math"
	"os"
	"sync"
	"time"
)

var gateLog *log.Logger
var gateLogOnce sync.Once

func InitGateLog() {
	gateLogOnce.Do(func() {
		file, err := os.OpenFile("gates.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return
		}
		gateLog = log.New(file, "", log.Ltime|log.Lmicroseconds)
	})
}

type Gate struct {
	Position    int
	Restriction int
	Type        string
}

func Hadamard(state QuantumState) QuantumState {
	newAmp0 := (state.Amplitude + state.Phase) / math.Sqrt(2)
	newAmp1 := (state.Amplitude - state.Phase) / math.Sqrt(2)

	return QuantumState{
		Amplitude: math.Abs(newAmp0),
		Phase:     newAmp1,
		PushTime:  time.Now(),
	}
}

func PauliX(state QuantumState) QuantumState {
	return QuantumState{
		Amplitude: state.Phase,
		Phase:     state.Amplitude,
		PushTime:  time.Now(),
	}
}

func PauliZ(state QuantumState) QuantumState {
	return QuantumState{
		Amplitude: state.Amplitude,
		Phase:     -state.Phase,
		PushTime:  time.Now(),
	}
}

func CNOT(control, target QuantumState) QuantumState {
	if control.Amplitude > 0.5 {
		return PauliX(target)
	}
	return target
}

func (g *Gate) Apply(pressure int, chainID string) bool {

	if pressure < g.Restriction/32 {
		return false
	}

	waveMu.Lock()
	defer waveMu.Unlock()

	switch g.Type {
	case "hadamard":
		WaveOrigin = Hadamard(WaveOrigin)
		if gateLog != nil {
			gateLog.Printf("[%s] HADAMARD fired pressure=%d A=%.3f P=%.3f",
				chainID, pressure, WaveOrigin.Amplitude, WaveOrigin.Phase)
		}
		return true

	case "paulix":
		WaveOrigin = PauliX(WaveOrigin)
		if gateLog != nil {
			gateLog.Printf("[%s] PAULIX fired pressure=%d A=%.3f P=%.3f",
				chainID, pressure, WaveOrigin.Amplitude, WaveOrigin.Phase)
		}
		return true

	case "pauliz":
		WaveOrigin = PauliZ(WaveOrigin)
		if gateLog != nil {
			gateLog.Printf("[%s] PAULIZ fired pressure=%d A=%.3f P=%.3f",
				chainID, pressure, WaveOrigin.Amplitude, WaveOrigin.Phase)
		}
		return true
	}

	return false
}

func (g *Gate) StartGateMonitor(chainID string, getPressure func() int, stopCh <-chan struct{}) {
	freq := GetChainFrequency(chainID)
	if freq == 0 {
		freq = 1.0
	}

	interval := time.Duration(float64(time.Second) / freq)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pressure := getPressure()
			g.Apply(pressure, chainID)
		case <-stopCh:
			return
		}
	}
}
