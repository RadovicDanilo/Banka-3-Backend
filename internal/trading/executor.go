package trading

import (
	"context"
	"errors"
	"log"
	"math"
	"math/rand"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// Market-order execution engine (#205). Per spec pp. 51, 57–58 the engine
// fills approved market orders in randomly sized chunks at randomly spaced
// intervals, with the interval scaled by the listing's daily volume. The
// implementation is a single ticker goroutine rather than per-order
// goroutines so cancellation/shutdown stays simple and state (next-fill
// time per order) lives in one map. In-memory state is intentional:
// restart re-rolls delays, which is acceptable at sim fidelity.
const (
	executorTickInterval = 1 * time.Second
	// executorDefaultDelaySeconds is used when today's listing volume is
	// still 0 and the spec formula (24*60 * remaining / Volume) would
	// divide by zero. Picked short enough that the first fill happens
	// quickly, after which volume becomes non-zero and the formula takes
	// over on its own.
	executorDefaultDelaySeconds int64 = 30
	// afterHoursDelayBonus matches the spec's "add 30 min" rule on top of
	// the computed interval for orders flagged as after-hours (p.58).
	afterHoursDelayBonus = 30 * time.Minute
	// executorFailureBackoff keeps a persistently failing order from
	// hot-looping on the tick queue without dropping it permanently.
	executorFailureBackoff = 30 * time.Second
)

// StartExecutor launches the market-order execution worker. Mirrors the
// cancel-func pattern used by bank.StartScheduler so main.go treats both
// the same way.
func (s *Server) StartExecutor() func() {
	ctx, cancel := context.WithCancel(context.Background())
	go s.runExecutor(ctx)
	return cancel
}

func (s *Server) runExecutor(ctx context.Context) {
	nextFillAt := map[int64]time.Time{}
	ticker := time.NewTicker(executorTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.executorTick(now, nextFillAt)
		}
	}
}

// executorTick scans the approved market-order pool and fires any whose
// scheduled fill time has elapsed. Listings only for now: options and forex
// have no ask/bid + listing_daily_price_info.volume, so they need their own
// engine and are out of scope for #205.
func (s *Server) executorTick(now time.Time, nextFillAt map[int64]time.Time) {
	var orders []Order
	err := s.db.Where(
		"status = ? AND is_done = ? AND order_type = ? AND listing_id IS NOT NULL",
		StatusApproved, false, OrderMarket,
	).Find(&orders).Error
	if err != nil {
		log.Printf("[Executor] ERROR loading orders: %v", err)
		return
	}

	alive := make(map[int64]struct{}, len(orders))
	for _, o := range orders {
		alive[o.ID] = struct{}{}
	}
	for id := range nextFillAt {
		if _, ok := alive[id]; !ok {
			delete(nextFillAt, id)
		}
	}

	for i := range orders {
		o := &orders[i]
		due, scheduled := nextFillAt[o.ID]
		if !scheduled {
			// New order seen this tick: schedule immediately so the first
			// fill happens next tick instead of waiting on a delay
			// computed against zero progress.
			nextFillAt[o.ID] = now
			continue
		}
		if now.Before(due) {
			continue
		}
		next, err := s.executeFill(o, now)
		if err != nil {
			log.Printf("[Executor] order %d fill failed: %v", o.ID, err)
			nextFillAt[o.ID] = now.Add(executorFailureBackoff)
			continue
		}
		if next.IsZero() {
			delete(nextFillAt, o.ID)
			continue
		}
		nextFillAt[o.ID] = next
	}
}

// executeFill fills one chunk in a transaction. Returns the scheduled time
// for the next chunk; the zero time signals "order is complete, drop it".
func (s *Server) executeFill(o *Order, now time.Time) (time.Time, error) {
	var next time.Time
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// Re-lock + re-check: a concurrent cancel or prior fill on the same
		// order might have moved the row out from under this tick.
		locked, err := lockOrder(tx, o.ID)
		if err != nil {
			return err
		}
		if locked.Status != StatusApproved || locked.IsDone || locked.RemainingPortions <= 0 {
			next = time.Time{}
			return nil
		}
		if locked.OrderType != OrderMarket || locked.ListingID == nil {
			next = time.Time{}
			return nil
		}

		var listing Listing
		if err := tx.First(&listing, *locked.ListingID).Error; err != nil {
			return status.Errorf(codes.Internal, "%v", err)
		}
		var exch Exchange
		if err := tx.First(&exch, listing.ExchangeID).Error; err != nil {
			return status.Errorf(codes.Internal, "%v", err)
		}

		// Clock check: the delay may have fired while the exchange is
		// closed (e.g. order was scheduled late in the session and slept
		// past close). Postpone one tick rather than filling against a
		// stale quote.
		if !IsOpen(exch, now) {
			next = now.Add(executorTickInterval)
			return nil
		}

		fillPPU := fillPricePerUnit(locked.Direction, listing)
		if fillPPU <= 0 {
			next = now.Add(executorTickInterval)
			return nil
		}
		chunk := randomChunk(locked.RemainingPortions)
		chunkCostInstr := chunk * locked.ContractSize * fillPPU

		acc, err := s.bank.GetAccountByNumber(locked.AccountNumber)
		if err != nil {
			return status.Errorf(codes.Internal, "%v", err)
		}

		instrCurrency := exch.Currency
		settle, err := s.planSettlement(acc.Currency, instrCurrency, chunkCostInstr, locked.Direction)
		if err != nil {
			return err
		}

		if err := applySettlement(tx, locked.Direction, locked.AccountNumber, acc.Currency, instrCurrency, settle); err != nil {
			return err
		}

		fill := OrderFill{
			OrderID:      locked.ID,
			Portions:     chunk,
			PricePerUnit: fillPPU,
			FxRate:       settle.fxRate,
		}
		if err := tx.Create(&fill).Error; err != nil {
			return status.Errorf(codes.Internal, "%v", err)
		}

		newVolume, err := upsertDailyVolume(tx, *locked.ListingID, now, chunk, listing)
		if err != nil {
			return err
		}

		newRemaining := locked.RemainingPortions - chunk
		updates := map[string]any{
			"remaining_portions": newRemaining,
			"last_modification":  now,
		}
		if newRemaining <= 0 {
			updates["is_done"] = true
			updates["status"] = string(StatusDone)
		}
		if err := tx.Model(locked).Updates(updates).Error; err != nil {
			return status.Errorf(codes.Internal, "%v", err)
		}

		if newRemaining <= 0 {
			next = time.Time{}
			return nil
		}
		next = now.Add(nextDelay(newRemaining, newVolume, locked.AfterHours))
		return nil
	})
	if err != nil {
		return time.Time{}, err
	}
	return next, nil
}

// fillPricePerUnit returns the per-unit price the chunk should fill at:
// ask for a buy (placer pays the asking price), bid for a sell (placer gets
// the bid). Multiplied by contract_size downstream for the chunk total.
func fillPricePerUnit(d OrderDirection, l Listing) int64 {
	if d == DirectionBuy {
		return l.AskPrice
	}
	return l.BidPrice
}

// randomChunk picks the portion count for this fill: Random(1, remaining).
// Always at least 1 so progress is guaranteed when called on an order with
// work left.
func randomChunk(remaining int64) int64 {
	if remaining <= 1 {
		return remaining
	}
	return rand.Int63n(remaining) + 1
}

// nextDelay implements the spec formula: Random(0, 24*60 / (Volume /
// remaining)) seconds, with an after-hours bonus of 30 min. Volume==0 is a
// first-of-day edge case (no daily row yet) — we fall back to a short
// default so the first fill lands promptly and builds volume for later
// ticks to key off.
func nextDelay(remaining, volume int64, afterHours bool) time.Duration {
	var maxSec int64
	if volume <= 0 || remaining <= 0 {
		maxSec = executorDefaultDelaySeconds
	} else {
		maxSec = 1440 * remaining / volume
		if maxSec <= 0 {
			maxSec = 1
		}
	}
	d := time.Duration(rand.Int63n(maxSec+1)) * time.Second
	if afterHours {
		d += afterHoursDelayBonus
	}
	return d
}

// settlement captures a single chunk's money movement across currencies.
// feeInstrument is the chunk cost in the instrument's currency; accAmount
// is its equivalent in the placer account's currency (equal to
// feeInstrument for same-currency orders). fxRate is nil for same-currency
// and otherwise snapshots rateInstrRSD/rateAccRSD for audit.
type settlement struct {
	accAmount   int64
	feeInstr    int64
	fxRate      *float64
	direction   OrderDirection
	accCurrency string
	instrCurr   string
}

// planSettlement converts the instrument-currency chunk cost into the
// account's currency via the existing menjacnica rates-to-RSD service
// (bank.GetExchangeRateToRSD). Rounding sides with the bank: buyers round
// up, sellers round down, so the placer doesn't pocket a rounding penny on
// either side. No menjacnica fee is charged at fill — the placement
// commission already covered it (commission.go, spec pp.27, 57).
func (s *Server) planSettlement(accCurrency, instrCurrency string, chunkCostInstr int64, dir OrderDirection) (settlement, error) {
	out := settlement{
		feeInstr:    chunkCostInstr,
		direction:   dir,
		accCurrency: accCurrency,
		instrCurr:   instrCurrency,
	}
	if accCurrency == instrCurrency {
		out.accAmount = chunkCostInstr
		return out, nil
	}
	rateAccRSD, err := s.bank.GetExchangeRateToRSD(accCurrency)
	if err != nil {
		return settlement{}, status.Errorf(codes.Internal, "%v", err)
	}
	rateInstrRSD, err := s.bank.GetExchangeRateToRSD(instrCurrency)
	if err != nil {
		return settlement{}, status.Errorf(codes.Internal, "%v", err)
	}
	rate := rateInstrRSD / rateAccRSD
	if dir == DirectionBuy {
		out.accAmount = int64(math.Ceil(float64(chunkCostInstr) * rate))
	} else {
		out.accAmount = int64(math.Floor(float64(chunkCostInstr) * rate))
	}
	out.fxRate = &rate
	return out, nil
}

// applySettlement moves the money for one chunk. With no real orderbook we
// use the bank-system client (system@banka3.rs) as the standing
// counterparty: buys debit the placer and credit the system's
// instrument-currency account; sells do the reverse. The same per-currency
// system accounts back commission.go's fee pool, so we don't seed a
// dedicated trading-stub account.
func applySettlement(tx *gorm.DB, dir OrderDirection, account, accCurrency, instrCurrency string, s settlement) error {
	if dir == DirectionBuy {
		if err := debitPlacer(tx, account, s.accAmount); err != nil {
			return err
		}
		return creditFeeAccount(tx, instrCurrency, s.feeInstr)
	}
	// Sell: stub pays the placer. Debit the system account first so we
	// surface an insufficient-funds failure before mutating the placer.
	if err := debitSystemAccount(tx, instrCurrency, s.feeInstr); err != nil {
		return err
	}
	return creditPlacer(tx, account, s.accAmount)
}

func debitPlacer(tx *gorm.DB, account string, amount int64) error {
	if amount <= 0 {
		return nil
	}
	res := tx.Exec(
		`UPDATE accounts SET balance = balance - ? WHERE number = ? AND balance >= ?`,
		amount, account, amount,
	)
	if res.Error != nil {
		return status.Errorf(codes.Internal, "%v", res.Error)
	}
	if res.RowsAffected == 0 {
		return status.Error(codes.FailedPrecondition, "insufficient funds for fill")
	}
	return nil
}

func creditPlacer(tx *gorm.DB, account string, amount int64) error {
	if amount <= 0 {
		return nil
	}
	res := tx.Exec(
		`UPDATE accounts SET balance = balance + ? WHERE number = ?`,
		amount, account,
	)
	if res.Error != nil {
		return status.Errorf(codes.Internal, "%v", res.Error)
	}
	if res.RowsAffected == 0 {
		return status.Error(codes.FailedPrecondition, "placer account missing")
	}
	return nil
}

// debitSystemAccount mirrors creditFeeAccount but in the opposite
// direction: used by sell-side fills to pull funds out of the bank-stub
// counterparty. Guards on balance so an under-funded stub surfaces as
// FailedPrecondition rather than silently going negative.
func debitSystemAccount(tx *gorm.DB, currency string, amount int64) error {
	if amount <= 0 {
		return nil
	}
	var feeAccount string
	err := tx.Raw(
		`SELECT a.number FROM accounts a
		 JOIN clients c ON c.id = a.owner
		 WHERE c.email = ? AND a.currency = ?
		 LIMIT 1`,
		bankSystemOwnerEmail, currency,
	).Scan(&feeAccount).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return status.Errorf(codes.FailedPrecondition, "no bank stub account for %s", currency)
		}
		return status.Errorf(codes.Internal, "%v", err)
	}
	if feeAccount == "" {
		return status.Errorf(codes.FailedPrecondition, "no bank stub account for %s", currency)
	}
	res := tx.Exec(
		`UPDATE accounts SET balance = balance - ? WHERE number = ? AND balance >= ?`,
		amount, feeAccount, amount,
	)
	if res.Error != nil {
		return status.Errorf(codes.Internal, "%v", res.Error)
	}
	if res.RowsAffected == 0 {
		return status.Errorf(codes.FailedPrecondition, "bank stub account underfunded for %s", currency)
	}
	return nil
}

// upsertDailyVolume bumps today's listing_daily_price_info.volume by the
// fill size, creating today's row from the listing's current quote if it
// doesn't exist yet. Returns the post-update volume so the executor can
// feed it into the delay formula without an extra SELECT.
func upsertDailyVolume(tx *gorm.DB, listingID int64, now time.Time, chunk int64, listing Listing) (int64, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var volume int64
	err := tx.Raw(
		`INSERT INTO listing_daily_price_info (listing_id, date, price, ask_price, bid_price, change, volume)
		 VALUES (?, ?, ?, ?, ?, 0, ?)
		 ON CONFLICT (listing_id, date)
		 DO UPDATE SET volume = listing_daily_price_info.volume + EXCLUDED.volume
		 RETURNING volume`,
		listingID, today, listing.Price, listing.AskPrice, listing.BidPrice, chunk,
	).Scan(&volume).Error
	if err != nil {
		return 0, status.Errorf(codes.Internal, "%v", err)
	}
	return volume, nil
}
