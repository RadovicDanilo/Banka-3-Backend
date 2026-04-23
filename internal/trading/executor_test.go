package trading

import (
	"testing"
	"time"
)

func TestFillPricePerUnit(t *testing.T) {
	l := Listing{AskPrice: 12500, BidPrice: 12400}
	if got := fillPricePerUnit(DirectionBuy, l); got != 12500 {
		t.Errorf("buy: got %d, want ask 12500", got)
	}
	if got := fillPricePerUnit(DirectionSell, l); got != 12400 {
		t.Errorf("sell: got %d, want bid 12400", got)
	}
}

func TestRandomChunkBounds(t *testing.T) {
	cases := []struct {
		name      string
		remaining int64
	}{
		{"one", 1},
		{"small", 5},
		{"large", 1000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for i := 0; i < 200; i++ {
				got := randomChunk(c.remaining)
				if got < 1 || got > c.remaining {
					t.Fatalf("chunk %d out of [1,%d]", got, c.remaining)
				}
			}
		})
	}
}

func TestRandomChunkZero(t *testing.T) {
	// Not reachable from the executor (it skips is_done/remaining<=0 rows),
	// but we still want a deterministic answer: zero remaining means zero
	// chunk, no panic from rand.Int63n(0).
	if got := randomChunk(0); got != 0 {
		t.Errorf("chunk on 0 remaining: got %d, want 0", got)
	}
}

func TestNextDelayVolumeZeroUsesDefault(t *testing.T) {
	// Volume=0 happens on the very first fill of the day. Formula would
	// divide by zero; we fall back to executorDefaultDelaySeconds so the
	// first fill lands promptly and seeds volume for later ticks.
	for i := 0; i < 100; i++ {
		d := nextDelay(10, 0, false)
		if d < 0 || d > time.Duration(executorDefaultDelaySeconds)*time.Second {
			t.Fatalf("delay %s outside [0, %ds]", d, executorDefaultDelaySeconds)
		}
	}
}

func TestNextDelayFormula(t *testing.T) {
	// remaining=100, volume=1440 → max = 1440*100/1440 = 100 seconds.
	max := 100 * time.Second
	for i := 0; i < 200; i++ {
		d := nextDelay(100, 1440, false)
		if d < 0 || d > max {
			t.Fatalf("delay %s outside [0, %s]", d, max)
		}
	}
}

func TestNextDelayAfterHoursBonus(t *testing.T) {
	// After-hours: every computed delay gets +30 min.
	for i := 0; i < 100; i++ {
		d := nextDelay(100, 1440, true)
		if d < afterHoursDelayBonus {
			t.Fatalf("after-hours delay %s below bonus floor %s", d, afterHoursDelayBonus)
		}
		if d > afterHoursDelayBonus+100*time.Second {
			t.Fatalf("after-hours delay %s above ceiling", d)
		}
	}
}

func TestNextDelayHighVolumeClampsToOneSecond(t *testing.T) {
	// Volume >> remaining * 1440 → integer division yields 0. The helper
	// bumps the max to 1s so rand.Int63n doesn't panic on zero.
	for i := 0; i < 100; i++ {
		d := nextDelay(1, 1_000_000_000, false)
		if d < 0 || d > time.Second {
			t.Fatalf("delay %s outside [0, 1s]", d)
		}
	}
}

func TestPlanSettlementSameCurrency(t *testing.T) {
	// Same-currency orders leave rate unset and copy the instrument-
	// currency cost straight through.
	s := &Server{}
	got, err := s.planSettlement("USD", "USD", 12500, DirectionBuy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.accAmount != 12500 || got.feeInstr != 12500 {
		t.Errorf("accAmount=%d feeInstr=%d, want 12500/12500", got.accAmount, got.feeInstr)
	}
	if got.fxRate != nil {
		t.Errorf("fxRate should be nil for same-currency, got %v", *got.fxRate)
	}
}
