package trading

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// maintenanceMargin computes the maintenance margin in instrument-native minor
// units for a position of the given quantity (spec pp.47–49). The formula is
// uniform once MarginBasePrice / MarginPermille are resolved per security
// type (see resolveInstrument):
//
//	stock   : 1            × price       × 50%  × qty
//	future  : contract_size× price       × 10%  × qty
//	forex   : 1000         × price       × 10%  × qty
//	option  : 1            × stock_price × 50%  × qty
func maintenanceMargin(contractSize, basePrice, quantity, permille int64) int64 {
	return contractSize * basePrice * quantity * permille / 1000
}

// initialMarginCost returns the Initial Margin Cost in the same units as the
// input: IMC = Maintenance Margin × 1.1 (spec p.56).
func initialMarginCost(mm int64) int64 {
	return mm * 11 / 10
}

// callerHasMarginPermission checks whether the given employee email has the
// `margin_trading` permission (admin bypasses). Used to gate employee-placed
// margin orders per spec p.56.
func callerHasMarginPermission(db *gorm.DB, email string) bool {
	if email == "" {
		return false
	}
	var count int64
	err := db.Table("employees").
		Joins("JOIN employee_permissions ep ON ep.employee_id = employees.id").
		Joins("JOIN permissions p ON p.id = ep.permission_id").
		Where("employees.email = ? AND p.name IN (?)", email, []string{"admin", "margin_trading"}).
		Count(&count).Error
	if err != nil {
		return false
	}
	return count > 0
}

// checkMarginEligibility enforces the spec p.56 rules when an order is placed
// on margin. Clients qualify if they hold any approved loan whose remaining
// debt (converted to RSD) exceeds IMC_RSD; otherwise — and always for
// employee placers — the debit account's balance must exceed IMC in the
// instrument's native currency. Rejects with InvalidArgument when neither
// condition holds.
func (s *Server) checkMarginEligibility(tx *gorm.DB, clientID int64, isClient bool, debitBalance int64, info *instrumentInfo, quantity int64) error {
	mm := maintenanceMargin(info.ContractSize, info.MarginBasePrice, quantity, info.MarginPermille)
	imcNative := initialMarginCost(mm)

	if isClient {
		imcRSD, err := s.approxPriceRSD(info.Currency, 1, imcNative, 1)
		if err != nil {
			return err
		}
		ok, err := clientHasQualifyingLoan(tx, s.GetExchangeRateToRSD, clientID, imcRSD)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}

	if debitBalance > imcNative {
		return nil
	}
	return status.Error(codes.InvalidArgument,
		"margin eligibility failed: debit account balance and approved loans do not cover the initial margin cost")
}

// clientHasQualifyingLoan returns true when the client has any approved loan
// whose remaining_debt (converted to RSD via the bank's menjacnica rate)
// strictly exceeds imcRSD. Loans denominated in RSD skip the lookup.
func clientHasQualifyingLoan(tx *gorm.DB, rateFn func(string) (float64, error), clientID int64, imcRSD int64) (bool, error) {
	var rows []struct {
		RemainingDebt int64  `gorm:"column:remaining_debt"`
		Currency      string `gorm:"column:currency"`
	}
	err := tx.Raw(`
		SELECT l.remaining_debt, cur.label AS currency
		FROM loans l
		JOIN accounts a ON a.id = l.account_id
		JOIN currencies cur ON cur.id = l.currency_id
		WHERE a.owner = ? AND l.loan_status = 'approved'
	`, clientID).Scan(&rows).Error
	if err != nil {
		return false, status.Errorf(codes.Internal, "%v", err)
	}
	for _, r := range rows {
		debtRSD := r.RemainingDebt
		if r.Currency != "RSD" {
			rate, err := rateFn(r.Currency)
			if err != nil {
				return false, status.Errorf(codes.Internal, "failed to load exchange rate for %s: %v", r.Currency, err)
			}
			debtRSD = int64(float64(r.RemainingDebt) * rate)
		}
		if debtRSD > imcRSD {
			return true, nil
		}
	}
	return false, nil
}
