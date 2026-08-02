package api

import (
	"testing"
	"time"
)

func TestNextSerial(t *testing.T) {
	today := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		current uint32
		want    uint32
	}{
		{"same-day increment", 2026080101, 2026080102},
		{"same-day high counter", 2026080142, 2026080143},
		{"counter overflow rolls the date", 2026080199, 2026080200},
		{"stale date resets to today", 2025010199, 2026080101},
		{"future date keeps incrementing", 2026123101, 2026123102},
		{"zero serial starts at today01", 0, 2026080101},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextSerial(tt.current, today); got != tt.want {
				t.Errorf("NextSerial(%d) = %d, want %d", tt.current, got, tt.want)
			}
		})
	}
}
