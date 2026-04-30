package server

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/RAF-SI-2025/Banka-3-Backend/pkg/logger"
	userpb "github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto/user"
	"github.com/RAF-SI-2025/Banka-3-Backend/services/user/internal/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Login(ctx context.Context, req *userpb.LoginRequest) (*userpb.LoginResponse, error) {
	l := logger.FromContext(ctx).With("email", req.Email)
	user, err := s.repo.GetUserByEmail(req.Email)
	if err != nil || user == nil {
		l.WarnContext(ctx, "audit: login failed", "reason", "unknown user")
		return nil, status.Error(codes.Unauthenticated, "wrong creds")
	}
	hashedPassword := HashPassword(req.Password, user.Salt)

	if bytes.Equal(hashedPassword, user.HashedPassword) {
		role, permissions, active := s.getRoleAndPermissions(user.Email)

		if !active {
			l.WarnContext(ctx, "audit: login failed", "reason", "deactivated")
			return nil, status.Error(codes.Unauthenticated, "account deactivated")
		}

		accessToken, err := s.GenerateAccessToken(user.Email)
		if err != nil {
			return nil, err
		}
		refreshToken, err := s.GenerateRefreshToken(user.Email)
		if err != nil {
			return nil, err
		}
		err = s.repo.InsertRefreshToken(refreshToken)
		if err != nil {
			return nil, err
		}

		if err := s.CreateSession(ctx, user.Email, SessionData{
			Role:        role,
			Permissions: permissions,
			Active:      true,
		}); err != nil {
			return nil, status.Error(codes.Unavailable, "session store unavailable")
		}

		l.InfoContext(ctx, "audit: login success", "role", role)
		return &userpb.LoginResponse{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			Permissions:  permissions,
			Role:         role,
		}, nil
	}

	l.WarnContext(ctx, "audit: login failed", "reason", "wrong password")
	return nil, status.Error(codes.Unauthenticated, "wrong creds")
}

func (s *Server) Logout(ctx context.Context, req *userpb.LogoutRequest) (*userpb.LogoutResponse, error) {
	email := req.Email
	tx, err := s.database.Begin()
	if err != nil {
		return nil, fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	err = s.repo.RevokeRefreshTokensByEmail(tx, email)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	_ = s.DeleteSession(ctx, email)

	logger.FromContext(ctx).InfoContext(ctx, "audit: logout", "email", email)
	return &userpb.LogoutResponse{
		Success: true,
	}, nil
}

// EnsureAdminImpliesSupervisor returns perms with "supervisor" appended when
// "admin" is present but "supervisor" is not (spec p.38: admin is-a supervisor).
// Idempotent: calling twice yields the same result.
func EnsureAdminImpliesSupervisor(perms []string) []string {
	set := NamesToSet(perms)
	if _, hasAdmin := set["admin"]; !hasAdmin {
		return perms
	}
	if _, hasSup := set["supervisor"]; hasSup {
		return perms
	}
	return append(perms, "supervisor")
}

// TogglesTradingRole reports whether the `agent` or `supervisor` membership differs
// between the old and new permission sets.
func TogglesTradingRole(oldSet, newSet map[string]struct{}) bool {
	for _, perm := range []string{"agent", "supervisor"} {
		_, inOld := oldSet[perm]
		_, inNew := newSet[perm]
		if inOld != inNew {
			return true
		}
	}
	return false
}

func callerIsAdmin(_ context.Context, s *Server, callerEmail string) bool {
	if strings.TrimSpace(callerEmail) == "" {
		return false
	}
	caller, err := s.repo.GetEmployeeByAttribute("email", callerEmail)
	if err != nil || caller == nil {
		return false
	}
	for _, p := range caller.Permissions {
		if p.Name == "admin" {
			return true
		}
	}
	return false
}

func callerCanManageLimits(_ context.Context, s *Server, callerEmail string) bool {
	if strings.TrimSpace(callerEmail) == "" {
		return false
	}
	caller, err := s.repo.GetEmployeeByAttribute("email", callerEmail)
	if err != nil || caller == nil {
		return false
	}
	for _, p := range caller.Permissions {
		if p.Name == "admin" || p.Name == "supervisor" {
			return true
		}
	}
	return false
}

func (s *Server) GetUserPermissions(ctx context.Context, req *userpb.GetUserPermissionsRequest) (*userpb.GetUserPermissionsResponse, error) {
	session, err := s.GetSession(ctx, req.Email)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "session store unavailable")
	}
	if session == nil {
		return nil, status.Error(codes.Unauthenticated, "no active session")
	}

	return &userpb.GetUserPermissionsResponse{
		Role:        session.Role,
		Permissions: session.Permissions,
	}, nil
}

// getRoleAndPermissions determines the role, permissions, and active status for a user by email.
// Employees get role "employee" with their DB permissions; clients get role "client" with empty permissions.
// The active flag is only meaningful for employees; clients always return true.
func (s *Server) getRoleAndPermissions(email string) (role string, permissions []string, active bool) {
	emp, err := s.repo.GetEmployeeByAttribute("email", email)
	if err == nil && emp != nil {
		permissions := make([]string, len(emp.Permissions))
		for i, v := range emp.Permissions {
			permissions[i] = v.Name
		}
		return "employee", permissions, emp.Active
	}
	return "client", []string{}, true
}

// permissionSet converts a list of Permission rows to a string set for easy membership tests.
func permissionSet(perms []model.Permission) map[string]struct{} {
	out := make(map[string]struct{}, len(perms))
	for _, p := range perms {
		out[p.Name] = struct{}{}
	}
	return out
}
