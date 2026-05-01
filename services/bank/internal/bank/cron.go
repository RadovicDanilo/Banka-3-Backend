package bank

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/RAF-SI-2025/Banka-3-Backend/pkg/logger"
	"gorm.io/gorm"
)

// StartScheduler kicks off background jobs for loan stuff, returns a cancel func for cleanup
func (s *Server) StartScheduler() func() {
	ctx, cancel := context.WithCancel(context.Background())

	go s.runOnSchedule(ctx, 2, isFirstOfMonth, s.RunMonthlyVariableRateUpdate)
	go s.runOnSchedule(ctx, 6, always, s.RunDailyInstallmentCollection)

	return cancel
}

func always(time.Time) bool           { return true }
func isFirstOfMonth(t time.Time) bool { return t.Day() == 1 }

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

// RunMonthlyVariableRateUpdate recalculates rates for variable loans on the 1st of each month
func (s *Server) RunMonthlyVariableRateUpdate() {
	start := time.Now()
	l := logger.L().With("job", "monthly_variable_rate_update")
	l.Info("cron start")

	loans, err := s.getApprovedVariableLoans()
	if err != nil {
		l.Error("fetching variable loans failed", "err", err)
		return
	}

	var updated, skipped, failed int
	defer func() {
		l.Info("cron end",
			"total", len(loans),
			"updated", updated,
			"skipped", skipped,
			"failed", failed,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}()

	for _, loan := range loans {
		currencyLabel, err := s.getCurrencyLabelByID(loan.Currency_id)
		if err != nil {
			l.Error("getting currency for loan failed", "loan_id", loan.Id, "err", err)
			failed++
			continue
		}

		rateToRSD, err := s.getExchangeRateToRSD(currencyLabel)
		if err != nil {
			l.Error("getting exchange rate failed", "currency", currencyLabel, "err", err)
			failed++
			continue
		}

		amountRSD := int64(float64(loan.Amount) * rateToRSD)
		baseRate := BaseAnnualRate(amountRSD)
		// spec says random for simulation, should probably be tied to EURIBOR or something
		offset := -1.50 + rand.Float64()*3.0
		newAnnualRate := baseRate + offset + MarginForLoanType(loan.Type)

		remainingMonths := loan.Installments - int64(s.countPaidInstallments(loan.Id))
		if remainingMonths <= 0 {
			skipped++
			continue
		}

		newPayment := CalculateAnnuity(loan.Remaining_debt, newAnnualRate, remainingMonths)

		err = s.db_gorm.Model(&Loan{}).Where("id = ?", loan.Id).Updates(map[string]any{
			"interest_rate":   float32(newAnnualRate),
			"monthly_payment": newPayment,
		}).Error
		if err != nil {
			l.Error("updating loan failed", "loan_id", loan.Id, "err", err)
			failed++
			continue
		}

		updated++
		l.Debug("updated variable loan", "loan_id", loan.Id, "rate", newAnnualRate, "payment", newPayment)
	}
}

// RunDailyInstallmentCollection daily job: collect payments from due loans, retry late ones after 3 days
func (s *Server) RunDailyInstallmentCollection() {
	start := time.Now()
	l := logger.L().With("job", "daily_installment_collection")
	l.Info("cron start")
	today := time.Now().Truncate(24 * time.Hour)

	loans, err := s.getLoansDueForCollection(today)
	if err != nil {
		l.Error("fetching due loans failed", "err", err)
		return
	}

	var retryCount int
	defer func() {
		l.Info("cron end",
			"due_processed", len(loans),
			"late_retried", retryCount,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}()

	for i := range loans {
		s.processLoanPayment(&loans[i], today, false)
	}

	// retry late ones - give them 3 days grace period before we bug them again
	var lateInstallments []LoanInstallment
	err = s.db_gorm.
		Where("status = ? AND due_date <= ?", Installment_Late, today.AddDate(0, 0, -3)).
		Find(&lateInstallments).Error
	if err != nil {
		l.Error("fetching late installments for retry failed", "err", err)
		return
	}

	retried := make(map[int64]bool)
	for _, inst := range lateInstallments {
		if retried[inst.Loan_id] {
			continue
		}
		retried[inst.Loan_id] = true
		retryCount++

		var loan Loan
		if err := s.db_gorm.First(&loan, inst.Loan_id).Error; err != nil {
			l.Error("fetching loan for retry failed", "loan_id", inst.Loan_id, "err", err)
			continue
		}
		s.processLoanPayment(&loan, today, true)
	}
}

func (s *Server) processLoanPayment(loan *Loan, today time.Time, isRetry bool) {
	l := logger.L().With("job", "process_loan_payment", "loan_id", loan.Id, "account_id", loan.Account_id, "retry", isRetry)

	// Deduct the installment from the client's account
	result := s.db_gorm.Model(&Account{}).
		Where("id = ? AND balance >= ?", loan.Account_id, loan.Monthly_payment).
		Update("balance", gorm.Expr("balance - ?", loan.Monthly_payment))
	paymentSucceeded := result.Error == nil && result.RowsAffected > 0

	if paymentSucceeded {
		l.Info("deducted installment", "amount", loan.Monthly_payment)
	} else {
		l.Warn("deduction failed", "amount", loan.Monthly_payment, "err", result.Error)
	}

	if paymentSucceeded {
		installment := LoanInstallment{
			Loan_id:            loan.Id,
			Installment_amount: loan.Monthly_payment,
			Interest_rate:      loan.Interest_rate,
			Currency_id:        loan.Currency_id,
			Due_date:           loan.Next_payment_due,
			Paid_date:          today,
			Status:             Installment_Paid,
		}
		if err := s.db_gorm.Create(&installment).Error; err != nil {
			l.Error("creating paid installment failed", "err", err)
			return
		}

		newDebt := loan.Remaining_debt - loan.Monthly_payment
		if newDebt < 0 {
			newDebt = 0
		}

		updates := map[string]any{
			"remaining_debt":   newDebt,
			"next_payment_due": loan.Next_payment_due.AddDate(0, 1, 0),
		}
		if newDebt <= 0 {
			updates["loan_status"] = Paid
		}

		if err := s.db_gorm.Model(&Loan{}).Where("id = ?", loan.Id).Updates(updates).Error; err != nil {
			l.Error("updating loan after payment failed", "err", err)
		}

		l.Info("payment recorded", "remaining_debt", newDebt)

		currencyLabel, _ := s.getCurrencyLabelByID(loan.Currency_id)
		email, _ := s.getClientEmailByAccountID(loan.Account_id)
		if email != "" {
			_ = s.sendLoanPaymentSuccessEmail(
				context.Background(),
				email,
				fmt.Sprintf("%d", loan.Id),
				fmt.Sprintf("%d", loan.Monthly_payment),
				currencyLabel,
			)
		}
	} else {
		installment := LoanInstallment{
			Loan_id:            loan.Id,
			Installment_amount: loan.Monthly_payment,
			Interest_rate:      loan.Interest_rate,
			Currency_id:        loan.Currency_id,
			Due_date:           loan.Next_payment_due,
			Paid_date:          time.Time{},
			Status:             Installment_Late,
		}
		if err := s.db_gorm.Create(&installment).Error; err != nil {
			l.Error("creating late installment failed", "err", err)
		}

		s.db_gorm.Model(&Loan{}).Where("id = ?", loan.Id).Update("loan_status", Late)

		// bijemo reket
		if isRetry {
			newRate := float32(float64(loan.Interest_rate) + 0.05)
			s.db_gorm.Model(&Loan{}).Where("id = ?", loan.Id).Update("interest_rate", newRate)
			l.Info("penalty applied", "new_rate", newRate)
		}

		// let them know their payment bounced
		currencyLabel, _ := s.getCurrencyLabelByID(loan.Currency_id)
		email, _ := s.getClientEmailByAccountID(loan.Account_id)
		if email != "" {
			_ = s.sendLoanPaymentFailedEmail(
				context.Background(),
				email,
				fmt.Sprintf("%d", loan.Id),
				fmt.Sprintf("%d", loan.Monthly_payment),
				currencyLabel,
				loan.Next_payment_due.Format("2006-01-02"),
			)
		}

		l.Warn("payment failed, status set to late")
	}
}
