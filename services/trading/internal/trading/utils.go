package trading

import (
	"context"
	"strings"

	exchangepb "github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto/exchange"
	userpb "github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto/user"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type callerIdentity struct {
	Email      string
	ClientID   int64
	IsClient   bool
	IsEmployee bool
}

// CallerIdentity is the exported form of callerIdentity, exposed for other
// services running in the bank process (e.g. trading) that reuse the same
// metadata-based auth scheme.
type CallerIdentity = callerIdentity

// ResolveCaller is exported for in-process callers that share bank's auth
// scheme. See authorizeAccountAccess for the ownership-check counterpart.
func (s *Server) ResolveCaller(ctx context.Context) (*CallerIdentity, error) {
	return s.resolveCaller(ctx)
}

// GetExchangeRateToRSD is exported for in-process callers (trading uses it to
// convert approximate order price into RSD for agent-limit accounting).
func (s *Server) GetExchangeRateToRSD(currencyLabel string) (float64, error) {
	rate, err := s.exchangeService.ConvertMoney(nil, &exchangepb.ConversionRequest{FromCurrency: currencyLabel, ToCurrency: "RSD", Amount: 1})
	if err != nil {
		return 0, err
	}
	return rate.ExchangeRate, nil
}

func (s *Server) resolveCaller(ctx context.Context) (*callerIdentity, error) {
	email, err := s.GetEmailFromMetadata(ctx)
	if err != nil {
		return nil, err
	}

	empResp, err := s.userService.GetEmployeeByEmail(ctx, &userpb.GetUserByEmailRequest{
		Email: email,
	})

	if err == nil && empResp != nil {
		return &callerIdentity{
			Email:      email,
			IsEmployee: true,
		}, nil
	}

	clientResp, err := s.userService.GetClients(ctx, &userpb.GetClientsRequest{
		Email: email,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to query user service")
	}
	if len(clientResp.Clients) == 0 {
		return nil, status.Error(codes.NotFound, "client not found")
	}

	return &callerIdentity{
		Email:    email,
		ClientID: clientResp.Clients[0].Id,
		IsClient: true,
	}, nil
}

func (s *Server) GetEmailFromMetadata(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "metadata missing")
	}

	emails := md.Get("user-email")
	if len(emails) == 0 || strings.TrimSpace(emails[0]) == "" {
		return "", status.Error(codes.Unauthenticated, "user-email missing")
	}

	return emails[0], nil
}
