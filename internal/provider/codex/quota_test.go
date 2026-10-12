package codex

import (
	"testing"
	"time"
)

func TestIdleWindowHasNoReset(t *testing.T) {
	now := time.Now()
	full, partial := 18000.0, 17985.0
	at := float64(now.Unix() + 18000)
	idle := usageWindow{UsedPercent: 0, LimitWindowSeconds: 18000, ResetAfterSeconds: &full, ResetAt: &at}
	if w := idle.toWindow("primary", now); w.ResetsAt != nil {
		t.Fatalf("an unstarted window should have no reset, got %v", w.ResetsAt)
	}
	started := usageWindow{UsedPercent: 0, LimitWindowSeconds: 18000, ResetAfterSeconds: &partial, ResetAt: &at}
	if w := started.toWindow("primary", now); w.ResetsAt == nil {
		t.Fatal("a started window should keep its reset even at 0% used")
	}
}
