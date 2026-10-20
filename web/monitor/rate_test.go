package monitor

import (
	"testing"
	"time"
)

func TestRateAcrossQuotaResets(t *testing.T) {
	var tracker Tracker
	at := time.Unix(100, 0)
	if _, ready := tracker.Sample(at, map[string]Counter{"inbound-443": {Up: 100, Down: 200}}); ready {
		t.Fatal("first sample must warm up")
	}
	tracker.Consumed(map[string]Counter{"inbound-443": {Up: 160, Down: 300}})
	tracker.Consumed(map[string]Counter{"inbound-443": {Up: 40, Down: 60}})
	rates, ready := tracker.Sample(at.Add(2*time.Second), map[string]Counter{"inbound-443": {Up: 20, Down: 40}})
	if got := rates["inbound-443"]; !ready || !got.Ready || got.Up != 60 || got.Down != 100 {
		t.Fatalf("rate across two resets: %+v, ready=%v", got, ready)
	}
	rates, _ = tracker.Sample(at.Add(4*time.Second), map[string]Counter{"inbound-443": {Up: 20, Down: 40}})
	if got := rates["inbound-443"]; !got.Ready || got.Up != 0 || got.Down != 0 {
		t.Fatalf("idle inbound: %+v", got)
	}
}

func TestRateWarmupAndCounterRollback(t *testing.T) {
	var tracker Tracker
	at := time.Unix(100, 0)
	tracker.Sample(at, map[string]Counter{"a": {Up: 100, Down: 200}})
	rates, _ := tracker.Sample(at.Add(2*time.Second), map[string]Counter{"a": {Up: 1, Down: 2}, "b": {Up: 1000}})
	if rates["a"].Ready || rates["b"].Ready {
		t.Fatal("rollback and new tags must establish a fresh baseline")
	}
	rates, _ = tracker.Sample(at.Add(4*time.Second), map[string]Counter{"a": {Up: 11, Down: 22}})
	if got := rates["a"]; !got.Ready || got.Up != 5 || got.Down != 10 {
		t.Fatalf("recovery after rollback: %+v", got)
	}
	if _, ready := tracker.Sample(at.Add(20*time.Second), nil); ready {
		t.Fatal("a long gap must discard the old interval")
	}
}

func TestResetToMissingIdleCounter(t *testing.T) {
	var tracker Tracker
	at := time.Unix(100, 0)
	tracker.Sample(at, map[string]Counter{"a": {Up: 100}})
	tracker.Consumed(map[string]Counter{"a": {Up: 200}})
	rates, _ := tracker.Sample(at.Add(2*time.Second), nil)
	if got := rates["a"]; !got.Ready || got.Up != 50 {
		t.Fatalf("bytes consumed before counter disappeared: %+v", got)
	}
}
