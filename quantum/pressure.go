package quantum

import (
	"sync"
	"time"
)


type PressurePoint struct {
	Location  BlockLocation
	Pressure  float64
	Timestamp time.Time
}


type PressureTracker struct {
	points map[BlockLocation]float64
	mu     sync.RWMutex

	
	totalEvents   int
	totalPressure float64
	lastUpdate    time.Time
}


func NewPressureTracker() *PressureTracker {
	return &PressureTracker{
		points:     make(map[BlockLocation]float64),
		lastUpdate: time.Now(),
	}
}


func (pt *PressureTracker) AddPressure(loc BlockLocation, amount float64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.points[loc] += amount
	pt.totalPressure += amount
	pt.totalEvents++
	pt.lastUpdate = time.Now()
}


func (pt *PressureTracker) GetPressure(loc BlockLocation) float64 {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return pt.points[loc]
}


func (pt *PressureTracker) GetTotalPressure() float64 {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return pt.totalPressure
}


func (pt *PressureTracker) GetEventCount() int {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return pt.totalEvents
}


func (pt *PressureTracker) GetPressureRate() float64 {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	elapsed := time.Since(pt.lastUpdate).Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(pt.totalEvents) / elapsed
}


func (pt *PressureTracker) GetHotspots(threshold float64) []PressurePoint {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	var hotspots []PressurePoint
	for loc, pressure := range pt.points {
		if pressure >= threshold {
			hotspots = append(hotspots, PressurePoint{
				Location:  loc,
				Pressure:  pressure,
				Timestamp: pt.lastUpdate,
			})
		}
	}

	return hotspots
}


func (pt *PressureTracker) GetTopPressure(n int) []PressurePoint {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	
	points := make([]PressurePoint, 0, len(pt.points))
	for loc, pressure := range pt.points {
		points = append(points, PressurePoint{
			Location:  loc,
			Pressure:  pressure,
			Timestamp: pt.lastUpdate,
		})
	}

	
	for i := 0; i < len(points); i++ {
		for j := i + 1; j < len(points); j++ {
			if points[j].Pressure > points[i].Pressure {
				points[i], points[j] = points[j], points[i]
			}
		}
	}

	
	if n > len(points) {
		n = len(points)
	}
	return points[:n]
}


func (pt *PressureTracker) Decay(halfLife time.Duration) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	elapsed := time.Since(pt.lastUpdate)
	if elapsed <= 0 {
		return
	}

	
	decayFactor := 0.5
	exponent := elapsed.Seconds() / halfLife.Seconds()
	multiplier := 1.0
	for i := 0.0; i < exponent; i += 1.0 {
		multiplier *= decayFactor
	}

	
	newTotal := 0.0
	for loc, pressure := range pt.points {
		decayed := pressure * multiplier
		pt.points[loc] = decayed
		newTotal += decayed
	}

	pt.totalPressure = newTotal
	pt.lastUpdate = time.Now()
}


func (pt *PressureTracker) Reset() {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.points = make(map[BlockLocation]float64)
	pt.totalEvents = 0
	pt.totalPressure = 0
	pt.lastUpdate = time.Now()
}


func (pt *PressureTracker) GetSnapshot() map[BlockLocation]float64 {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	snapshot := make(map[BlockLocation]float64)
	for k, v := range pt.points {
		snapshot[k] = v
	}
	return snapshot
}


func (pt *PressureTracker) StartDecayLoop(interval, halfLife time.Duration, stopCh <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pt.Decay(halfLife)
		case <-stopCh:
			return
		}
	}
}
