package trading

import (
	"testing"
	"time"

	tradingpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/trading"
)

func TestApprovalPricePerUnit(t *testing.T) {
	req := &tradingpb.CreateOrderRequest{LimitPrice: 150, StopPrice: 120}
	cases := []struct {
		name        string
		ot          OrderType
		marketPrice int64
		want        int64
	}{
		{"market uses listing quote", OrderMarket, 99, 99},
		{"limit uses limit value", OrderLimit, 99, 150},
		{"stop uses stop value", OrderStop, 99, 120},
		{"stop_limit uses limit value", OrderStopLimit, 99, 150},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := approvalPricePerUnit(c.ot, req, c.marketPrice); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestDecideOrderStatus(t *testing.T) {
	agent := agentLimits{Limit: 1000, UsedLimit: 200, NeedApproval: false}
	needApproval := agentLimits{Limit: 1000, UsedLimit: 0, NeedApproval: true}
	exhausted := agentLimits{Limit: 1000, UsedLimit: 1000, NeedApproval: false}

	cases := []struct {
		name           string
		role           callerRole
		limits         agentLimits
		approxRSD      int64
		pastSettlement bool
		want           OrderStatus
	}{
		{"past settlement declines all", roleClient, agentLimits{}, 0, true, StatusDeclined},
		{"client approved", roleClient, agentLimits{}, 100, false, StatusApproved},
		{"supervisor approved", roleSupervisor, agentLimits{}, 100, false, StatusApproved},
		{"other employee approved", roleOtherEmployee, agentLimits{}, 100, false, StatusApproved},
		{"agent within limit", roleAgent, agent, 500, false, StatusApproved},
		{"agent need_approval pending", roleAgent, needApproval, 10, false, StatusPending},
		{"agent used_limit reached pending", roleAgent, exhausted, 10, false, StatusPending},
		{"agent approx pushes over limit", roleAgent, agent, 900, false, StatusPending},
		{"agent exactly at limit approved", roleAgent, agent, 800, false, StatusApproved},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := decideOrderStatus(c.role, c.limits, c.approxRSD, c.pastSettlement)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestIsPastSettlement(t *testing.T) {
	now := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	yesterday := now.AddDate(0, 0, -1)
	today := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)
	tomorrow := now.AddDate(0, 0, 1)

	cases := []struct {
		name string
		sd   *time.Time
		want bool
	}{
		{"nil settlement (stock/forex) not past", nil, false},
		{"yesterday is past", &yesterday, true},
		{"today is not past", &today, false},
		{"tomorrow is not past", &tomorrow, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isPastSettlement(c.sd, now); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
