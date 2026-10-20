// Package monitor calculates rates from Xray counters without consuming traffic.
package monitor

import "time"

// Counter contains byte counters for one inbound tag.
type Counter struct {
	Up   int64
	Down int64
}

// Rate contains bytes per second, from the client's perspective.
type Rate struct {
	Up    float64 `json:"up"`
	Down  float64 `json:"down"`
	Ready bool    `json:"ready"`
}

// Tracker must be used under the same lock as Xray's read/reset operations.
type Tracker struct {
	previous map[string]Counter
	at       time.Time
}

// Consumed accounts for counters cleared by the existing quota collector.
// Subtracting from the baseline preserves bytes transferred across a reset.
func (t *Tracker) Consumed(counters map[string]Counter) {
	for tag, counter := range counters {
		if previous, ok := t.previous[tag]; ok {
			previous.Up -= counter.Up
			previous.Down -= counter.Down
			t.previous[tag] = previous
		}
	}
}

// Sample returns rates and whether a valid sampling interval is available.
// A long gap starts a new baseline instead of displaying an old average.
func (t *Tracker) Sample(now time.Time, counters map[string]Counter) (map[string]Rate, bool) {
	elapsed := now.Sub(t.at).Seconds()
	ready := !t.at.IsZero() && elapsed > 0 && elapsed <= 10
	rates := make(map[string]Rate)
	if ready {
		for tag, previous := range t.previous {
			current := counters[tag]
			up, down := current.Up-previous.Up, current.Down-previous.Down
			if up >= 0 && down >= 0 {
				rates[tag] = Rate{Up: float64(up) / elapsed, Down: float64(down) / elapsed, Ready: true}
			}
		}
	}
	// Explicitly warm up newly observed tags, including a tag that first receives traffic.
	for tag := range counters {
		if _, ok := rates[tag]; !ok {
			rates[tag] = Rate{}
		}
	}
	t.previous = make(map[string]Counter, len(counters))
	for tag, counter := range counters {
		t.previous[tag] = counter
	}
	t.at = now
	return rates, ready
}
