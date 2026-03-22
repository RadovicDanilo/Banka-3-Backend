package user

import (
	"crypto/rand"
	"encoding/base64"
	"time"

	userpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/user"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) GenerateRefreshToken(email string) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   email,
		ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour * 7)),
		IssuedAt:  jwt.NewNumericDate(now),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.refreshJwtSecret))
}

func (s *Server) GenerateAccessToken(email string) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   email,
		ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(now),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.accessJwtSecret))
}

func validateJWTToken(tokenString, secret string) (*userpb.ValidateTokenResponse, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	})
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}
	sub, _ := token.Claims.GetSubject()
	exp, _ := token.Claims.GetExpirationTime()
	iat, _ := token.Claims.GetIssuedAt()

	return &userpb.ValidateTokenResponse{
		Sub: sub,
		Exp: exp.Unix(),
		Iat: iat.Unix(),
	}, nil
}

func generateOpaqueToken() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}
