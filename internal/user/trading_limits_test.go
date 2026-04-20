package user

import (
	"context"
	"testing"

	userpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/user"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTogglesTradingRoleDetectsGrantRevoke(t *testing.T) {
	tests := []struct {
		name string
		old  []string
		new  []string
		want bool
	}{
		{"no change, no trading role", []string{"manage_loans"}, []string{"manage_loans", "manage_cards"}, false},
		{"grants agent", []string{"manage_loans"}, []string{"manage_loans", "agent"}, true},
		{"revokes agent", []string{"agent", "trade_stocks"}, []string{"trade_stocks"}, true},
		{"grants supervisor", []string{}, []string{"supervisor"}, true},
		{"revokes supervisor", []string{"supervisor"}, []string{}, true},
		{"agent stays set", []string{"agent"}, []string{"agent", "view_stocks"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := togglesTradingRole(namesToSet(tc.old), namesToSet(tc.new))
			if got != tc.want {
				t.Fatalf("togglesTradingRole(%v -> %v) = %v, want %v", tc.old, tc.new, got, tc.want)
			}
		})
	}
}

// UpdateEmployeeTradingLimit validates its arguments before touching the DB, so
// we can exercise those branches without plumbing gorm/sqlmock expectations.
func TestUpdateEmployeeTradingLimitArgValidation(t *testing.T) {
	server, _, db := newGormTestServer(t)
	defer func() { _ = db.Close() }()

	cases := []struct {
		name string
		req  *userpb.UpdateEmployeeTradingLimitRequest
		code codes.Code
	}{
		{
			name: "zero id",
			req:  &userpb.UpdateEmployeeTradingLimitRequest{Id: 0},
			code: codes.InvalidArgument,
		},
		{
			name: "nothing to update",
			req:  &userpb.UpdateEmployeeTradingLimitRequest{Id: 1},
			code: codes.InvalidArgument,
		},
		{
			name: "negative limit",
			req:  &userpb.UpdateEmployeeTradingLimitRequest{Id: 1, Limit: ptr64(-1)},
			code: codes.InvalidArgument,
		},
		{
			name: "negative used_limit",
			req:  &userpb.UpdateEmployeeTradingLimitRequest{Id: 1, UsedLimit: ptr64(-5)},
			code: codes.InvalidArgument,
		},
		{
			// permission check runs after validation; empty CallerEmail means no admin/supervisor
			// caller found → PermissionDenied without any DB round-trips.
			name: "caller not supervisor or admin",
			req:  &userpb.UpdateEmployeeTradingLimitRequest{Id: 1, Limit: ptr64(500)},
			code: codes.PermissionDenied,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := server.UpdateEmployeeTradingLimit(context.Background(), tc.req)
			if err == nil {
				t.Fatalf("expected error with code %s, got nil", tc.code)
			}
			if status.Code(err) != tc.code {
				t.Fatalf("got code %s, want %s (err=%v)", status.Code(err), tc.code, err)
			}
		})
	}
}

func ptr64(v int64) *int64 { return &v }
