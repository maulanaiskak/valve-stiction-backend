package main

import (
	"testing"
	"time"
)

func TestReverseInPlace_ChronologicalOrder(t *testing.T) {
	newest := time.Now()
	older := newest.Add(-1 * time.Second)
	oldest := newest.Add(-2 * time.Second)

	// DB returns newest-first (ORDER BY window_start DESC LIMIT n)
	out := []WindowSample{{WindowStart: newest}, {WindowStart: older}, {WindowStart: oldest}}
	reverseInPlace(out)

	if !out[0].WindowStart.Equal(oldest) || !out[2].WindowStart.Equal(newest) {
		t.Fatalf("expected chronological order (oldest first), got %v", out)
	}
}

func TestReverseInPlace_EmptyAndSingle(t *testing.T) {
	reverseInPlace(nil) // must not panic
	one := []WindowSample{{WindowStart: time.Now()}}
	reverseInPlace(one)
	if len(one) != 1 {
		t.Fatalf("expected single-element slice unchanged, got %v", one)
	}
}
