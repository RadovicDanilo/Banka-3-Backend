package trading

import (
	"context"
	"time"

	"github.com/RAF-SI-2025/Banka-3-Backend/pkg/logger"
)

// StartScheduler kicks off background jobs for loan stuff, returns a cancel func for cleanup
func (s *Server) StartScheduler() func() {
	ctx, cancel := context.WithCancel(context.Background())

	// Spec p.39: zero each agent's used_limit at end of day so the next day starts fresh.
	go s.runOnScheduleAt(ctx, 23, 59, always, s.RunDailyUsedLimitReset)
	// Spec p.63: capital-gains tax sweeps at the end of the calendar month.
	// Scheduled at 23:50 on the last day so it fires before the day-end
	// limit-reset at 23:59 (no overlap, deterministic ordering).
	go s.runOnScheduleAt(ctx, 23, 50, isLastOfMonth, s.RunMonthlyCapitalGainsCollection)

	return cancel
}

func always(time.Time) bool { return true }

// isLastOfMonth returns true when t is the calendar last day of its month —
// "tomorrow is the 1st". Robust against month length (28/29/30/31) without
// hard-coding day numbers.
func isLastOfMonth(t time.Time) bool { return t.AddDate(0, 0, 1).Day() == 1 }

// poor man's cron - wakes up at the target hour, runs fn if filter says yes
func (s *Server) runOnSchedule(ctx context.Context, hour int, filter func(time.Time) bool, fn func()) {
	s.runOnScheduleAt(ctx, hour, 0, filter, fn)
}

// runOnScheduleAt is the same as runOnSchedule but lets the caller specify the minute too.
func (s *Server) runOnScheduleAt(ctx context.Context, hour, minute int, filter func(time.Time) bool, fn func()) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case t := <-timer.C:
			if filter(t) {
				fn()
			}
		}
	}
}

// RunDailyUsedLimitReset zeroes used_limit on every employee row at end of day so
// the per-actuary daily trading limit refreshes for the next day (spec p.39).
func (s *Server) RunDailyUsedLimitReset() {
	l := logger.L().With("job", "daily_used_limit_reset")
	l.Info("cron start")
	res := s.db_gorm.Table("employees").
		Where("used_limit > 0").
		Updates(map[string]any{"used_limit": 0, "updated_at": time.Now()})
	if res.Error != nil {
		l.Error("resetting used_limit failed", "err", res.Error)
		return
	}
	l.Info("cron end", "rows", res.RowsAffected)
}
