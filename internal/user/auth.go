package user

import (
	"bytes"
	"context"
	"fmt"

	userpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/user"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Login(_ context.Context, req *userpb.LoginRequest) (*userpb.LoginResponse, error) {
	user, err := s.GetUserByEmail(req.Email)
	if err != nil || user == nil {
		return nil, status.Error(codes.Unauthenticated, "wrong creds")
	}
	hashedPassword := HashPassword(req.Password, user.salt)

	if bytes.Equal(hashedPassword, user.hashedPassword) {
		accessToken, err := s.GenerateAccessToken(user.email)
		if err != nil {
			return nil, err
		}
		refreshToken, err := s.GenerateRefreshToken(user.email)
		if err != nil {
			return nil, err
		}
		err = s.InsertRefreshToken(refreshToken)
		if err != nil {
			return nil, err
		}

		return &userpb.LoginResponse{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
		}, nil
	}
	return nil, status.Error(codes.Unauthenticated, "wrong creds")
}

func (s *Server) Logout(_ context.Context, req *userpb.LogoutRequest) (*userpb.LogoutResponse, error) {
	email := req.Email
	tx, err := s.database.Begin()
	if err != nil {
		return nil, fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	err = s.RevokeRefreshTokensByEmail(tx, email)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}
	return &userpb.LogoutResponse{Success: true}, nil
}

func (s *Server) Refresh(_ context.Context, req *userpb.RefreshRequest) (*userpb.RefreshResponse, error) {
	token, err := validateJWTToken(req.RefreshToken, s.refreshJwtSecret)
	if err != nil {
		return nil, err
	}
	email := token.Sub

	newSignedToken, err := s.GenerateRefreshToken(email)
	if err != nil {
		return nil, fmt.Errorf("generating refresh token: %w", err)
	}
	newAccessToken, err := s.GenerateAccessToken(email)
	if err != nil {
		return nil, fmt.Errorf("generating access token: %w", err)
	}

	newParsed, _, err := jwt.NewParser().ParseUnverified(newSignedToken, &jwt.RegisteredClaims{})
	if err != nil {
		return nil, fmt.Errorf("parsing new token: %w", err)
	}
	newExpiry, err := newParsed.Claims.GetExpirationTime()
	if err != nil {
		return nil, fmt.Errorf("getting expiry: %w", err)
	}

	tx, err := s.database.Begin()
	if err != nil {
		return nil, fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	err = s.rotateRefreshToken(tx, email, hashValue(req.RefreshToken), hashValue(newSignedToken), newExpiry.Time)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "wrong token")
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}
	return &userpb.RefreshResponse{AccessToken: newAccessToken, RefreshToken: newSignedToken}, nil
}

func (s *Server) ValidateRefreshToken(_ context.Context, req *userpb.ValidateTokenRequest) (*userpb.ValidateTokenResponse, error) {
	return validateJWTToken(req.Token, s.refreshJwtSecret)
}

func (s *Server) ValidateAccessToken(_ context.Context, req *userpb.ValidateTokenRequest) (*userpb.ValidateTokenResponse, error) {
	return validateJWTToken(req.Token, s.accessJwtSecret)
}
