package user

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	notificationpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/notification"
	userpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func (s *Server) RequestPasswordReset(ctx context.Context, req *userpb.PasswordActionRequest) (*userpb.PasswordActionResponse, error) {
	return s.requestPasswordAction(ctx, strings.TrimSpace(req.Email), passwordActionReset)
}

func (s *Server) RequestInitialPasswordSet(ctx context.Context, req *userpb.PasswordActionRequest) (*userpb.PasswordActionResponse, error) {
	return s.requestPasswordAction(ctx, strings.TrimSpace(req.Email), passwordActionInitialSet)
}

func (s *Server) SetPasswordWithToken(_ context.Context, req *userpb.SetPasswordWithTokenRequest) (*userpb.SetPasswordWithTokenResponse, error) {
	token := strings.TrimSpace(req.Token)
	newPassword := strings.TrimSpace(req.NewPassword)

	if token == "" || newPassword == "" {
		return nil, status.Error(codes.InvalidArgument, "token and new password are required")
	}

	tx, err := s.database.Begin()
	if err != nil {
		return nil, status.Error(codes.Internal, "starting transaction failed")
	}
	defer func() { _ = tx.Rollback() }()

	email, _, err := s.ConsumePasswordActionToken(tx, hashValue(token))
	if err != nil {
		if errors.Is(err, ErrInvalidPasswordActionToken) {
			return nil, status.Error(codes.InvalidArgument, "invalid or expired token")
		}
		return nil, status.Error(codes.Internal, "token validation failed")
	}

	user, err := s.GetUserByEmail(email)
	if err != nil || user == nil {
		return nil, status.Error(codes.Internal, "user lookup failed")
	}

	hashedPassword := HashPassword(newPassword, user.salt)
	if err := s.UpdatePasswordByEmail(tx, email, hashedPassword); err != nil {
		return nil, status.Error(codes.Internal, "password update failed")
	}
	if err := s.RevokeRefreshTokensByEmail(tx, email); err != nil {
		return nil, status.Error(codes.Internal, "refresh token revocation failed")
	}
	if err := tx.Commit(); err != nil {
		return nil, status.Error(codes.Internal, "committing transaction failed")
	}
	return &userpb.SetPasswordWithTokenResponse{Successful: true}, nil
}

func (s *Server) requestPasswordAction(ctx context.Context, email string, actionType string) (*userpb.PasswordActionResponse, error) {
	if email == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}

	user, err := s.GetUserByEmail(email)
	if err != nil {
		return nil, status.Error(codes.Internal, "user lookup failed")
	}
	if user == nil {
		return &userpb.PasswordActionResponse{Accepted: true}, nil
	}

	token, err := generateOpaqueToken()
	if err != nil {
		return nil, status.Error(codes.Internal, "token generation failed")
	}

	validUntil := time.Now().Add(resetPasswordTokenTTL)
	if actionType == passwordActionInitialSet {
		validUntil = time.Now().Add(initialSetPasswordTTL)
	}

	if err := s.UpsertPasswordActionToken(user.email, actionType, hashValue(token), validUntil); err != nil {
		return nil, status.Error(codes.Internal, "storing token failed")
	}

	baseURL := os.Getenv("PASSWORD_RESET_BASE_URL")
	if actionType == passwordActionInitialSet {
		baseURL = os.Getenv("PASSWORD_SET_BASE_URL")
	}
	link, err := buildPasswordLink(baseURL, token)
	if err != nil {
		return nil, status.Error(codes.Internal, "building password link failed")
	}

	if err := s.sendPasswordActionEmail(ctx, user.email, link, actionType); err != nil {
		return nil, status.Error(codes.Internal, "sending password email failed")
	}
	return &userpb.PasswordActionResponse{Accepted: true}, nil
}

func (s *Server) sendPasswordActionEmail(ctx context.Context, email string, link string, actionType string) error {
	notificationAddr := os.Getenv("NOTIFICATION_GRPC_ADDR")
	if strings.TrimSpace(notificationAddr) == "" {
		notificationAddr = defaultNotificationURL
	}

	conn, err := grpc.NewClient(notificationAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dialing notification service: %w", err)
	}
	defer func() { _ = conn.Close() }()

	client := notificationpb.NewNotificationServiceClient(conn)
	sendCtx, cancelSend := context.WithTimeout(ctx, 5*time.Second)
	defer cancelSend()

	req := &notificationpb.PasswordLinkMailRequest{ToAddr: email, Link: link}

	if actionType == passwordActionInitialSet {
		resp, err := client.SendInitialPasswordSetEmail(sendCtx, req)
		if err != nil {
			return fmt.Errorf("calling SendInitialPasswordSetEmail: %w", err)
		}
		if !resp.Successful {
			return fmt.Errorf("notification service reported unsuccessful initial set send")
		}
		return nil
	}

	resp, err := client.SendPasswordResetEmail(sendCtx, req)
	if err != nil {
		return fmt.Errorf("calling SendPasswordResetEmail: %w", err)
	}
	if !resp.Successful {
		return fmt.Errorf("notification service reported unsuccessful reset send")
	}
	return nil
}

func buildPasswordLink(baseURL string, token string) (string, error) {
	if strings.TrimSpace(baseURL) == "" {
		return "", fmt.Errorf("base URL is empty")
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parsing base URL: %w", err)
	}
	query := parsedURL.Query()
	query.Set("token", token)
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}
