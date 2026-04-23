package trading

import (
	"errors"
	"time"

	tradingpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/trading"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// callerRole classifies who is placing the order for status-routing purposes
// (spec p.51). Supervisor wins over agent when an employee happens to carry
// both permissions.
type callerRole int

const (
	roleClient callerRole = iota
	roleSupervisor
	roleAgent
	roleOtherEmployee
)

// agentLimits captures the employee-level knobs that gate an agent's orders.
// limit/used_limit are RSD minor units; need_approval forces every order to
// supervisor review regardless of headroom (spec p.39).
type agentLimits struct {
	Limit        int64
	UsedLimit    int64
	NeedApproval bool
}

// approvalPricePerUnit returns the unit price that feeds the approximate-price
// formula (spec p.57). Market orders use the current listing/option/forex
// quote; limit & stop-limit use the user-provided limit value; stop uses the
// stop value.
func approvalPricePerUnit(ot OrderType, req *tradingpb.CreateOrderRequest, marketPrice int64) int64 {
	switch ot {
	case OrderMarket:
		return marketPrice
	case OrderLimit, OrderStopLimit:
		return req.LimitPrice
	case OrderStop:
		return req.StopPrice
	}
	return 0
}

// decideOrderStatus encodes the spec p.51 routing table:
//   - expired underlying → decline immediately.
//   - client or supervisor placer → approve.
//   - agent placer → pending when need_approval is set, when used_limit has
//     already hit limit, or when this order would push the agent over limit.
//   - other employees (no agent/supervisor perms) → approve; the spec only
//     calls out agents for supervisor review.
func decideOrderStatus(role callerRole, limits agentLimits, approxRSD int64, pastSettlement bool) OrderStatus {
	if pastSettlement {
		return StatusDeclined
	}
	if role != roleAgent {
		return StatusApproved
	}
	if limits.NeedApproval {
		return StatusPending
	}
	if limits.UsedLimit >= limits.Limit {
		return StatusPending
	}
	if approxRSD+limits.UsedLimit > limits.Limit {
		return StatusPending
	}
	return StatusApproved
}

// isPastSettlement is centralized so time semantics stay consistent — we
// compare dates (a settlement on today's date hasn't passed yet).
func isPastSettlement(sd *time.Time, now time.Time) bool {
	if sd == nil {
		return false
	}
	return sd.Before(now.Truncate(24 * time.Hour))
}

// approxPriceRSD converts the native-currency approximate price to RSD minor
// units via the bank's menjacnica rate (no commission — spec p.57). RSD-native
// instruments skip the lookup to avoid a DB round-trip.
func (s *Server) approxPriceRSD(currency string, contractSize, pricePerUnit, quantity int64) (int64, error) {
	native := contractSize * pricePerUnit * quantity
	if currency == "RSD" {
		return native, nil
	}
	rate, err := s.bank.GetExchangeRateToRSD(currency)
	if err != nil {
		return 0, status.Errorf(codes.Internal, "failed to load exchange rate for %s: %v", currency, err)
	}
	return int64(float64(native) * rate), nil
}

// resolveEmployeeRole loads the placer's limits and classifies their trading
// role from their permission set. admin and supervisor map to roleSupervisor
// (admin implies supervisor per spec p.38 / #199), agent maps to roleAgent,
// anything else falls through to roleOtherEmployee.
func resolveEmployeeRole(tx *gorm.DB, email string) (int64, callerRole, agentLimits, error) {
	var emp struct {
		ID           int64
		Limit        int64 `gorm:"column:limit"`
		UsedLimit    int64 `gorm:"column:used_limit"`
		NeedApproval bool  `gorm:"column:need_approval"`
	}
	err := tx.Table("employees").
		Select(`id, "limit", used_limit, need_approval`).
		Where("email = ?", email).
		Take(&emp).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, 0, agentLimits{}, status.Error(codes.NotFound, "employee not found")
		}
		return 0, 0, agentLimits{}, status.Errorf(codes.Internal, "%v", err)
	}

	var perms []string
	err = tx.Table("permissions AS p").
		Joins("JOIN employee_permissions ep ON ep.permission_id = p.id").
		Where("ep.employee_id = ?", emp.ID).
		Pluck("p.name", &perms).Error
	if err != nil {
		return 0, 0, agentLimits{}, status.Errorf(codes.Internal, "%v", err)
	}

	limits := agentLimits{
		Limit:        emp.Limit,
		UsedLimit:    emp.UsedLimit,
		NeedApproval: emp.NeedApproval,
	}

	isSupervisor := false
	isAgent := false
	for _, p := range perms {
		switch p {
		case "admin", "supervisor":
			isSupervisor = true
		case "agent":
			isAgent = true
		}
	}
	switch {
	case isSupervisor:
		return emp.ID, roleSupervisor, limits, nil
	case isAgent:
		return emp.ID, roleAgent, limits, nil
	default:
		return emp.ID, roleOtherEmployee, limits, nil
	}
}
