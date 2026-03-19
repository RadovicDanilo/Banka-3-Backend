package bank

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"

	notificationpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/notification"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	bankpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/bank"
	userpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/user"
)

const (
	defaultNotificationURL = "notification:50051"
	cardRequestTokenTTL    = 24 * time.Hour
)

type Server struct {
	bankpb.UnimplementedBankServiceServer
	database *sql.DB
	db_gorm  *gorm.DB
}

func NewServer(database *sql.DB, gorm_db *gorm.DB) *Server {
	return &Server{
		database: database,
		db_gorm:  gorm_db,
	}
}

func generateOpaqueToken() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func hashValue(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func buildConfirmationLink(token string) (string, error) {
	baseURL := os.Getenv("CARD_CONFIRMATION_BASE_URL")
	if strings.TrimSpace(baseURL) == "" {
		return "", errors.New("CARD_CONFIRMATION_BASE_URL is not set")
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	query := parsedURL.Query()
	query.Set("token", token)
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func validateCreateCardInput(req *bankpb.CreateCardRequest) error {
	if strings.TrimSpace(req.AccountNumber) == "" || strings.TrimSpace(req.CardType) == "" || strings.TrimSpace(req.Name) == "" {
		return status.Error(codes.InvalidArgument, "account number, type and name are required")
	}
	if req.Limit <= 0 {
		return status.Error(codes.InvalidArgument, "limit must be greater than zero")
	}
	return nil
}

func (s *Server) CreateCard(ctx context.Context, req *bankpb.CreateCardRequest) (*bankpb.CardResponse, error) {
	if err := validateCreateCardInput(req); err != nil {
		return nil, err
	}

	var account Account
	if err := s.db_gorm.Where("number = ?", req.AccountNumber).First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "account not found")
		}
		return nil, status.Error(codes.Internal, "failed to fetch account")
	}

	// Provera limita (Max 2 za personal, unikatno po imenu za business)
	if err := s.checkCardLimits(&account, req); err != nil {
		return nil, err
	}

	cardNum, err := GenerateCardNumber(req.CardType, account.Number)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate card number")
	}

	newCard := Card{
		Number:         cardNum,
		Type:           card_type(req.CardType),
		Name:           req.Name,
		Creation_date:  time.Now(),
		Valid_until:    time.Now().AddDate(5, 0, 0),
		Account_number: account.Number,
		Cvv:            GenerateCVV(),
		Card_limit:     req.Limit,
		Status:         Active,
	}

	if err := s.db_gorm.Create(&newCard).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, status.Error(codes.AlreadyExists, "card number already exists")
		}
		return nil, status.Error(codes.Internal, "database error")
	}

	return &bankpb.CardResponse{
		CardNumber:     newCard.Number,
		CardType:       string(newCard.Type),
		CardName:       newCard.Name,
		CreationDate:   newCard.Creation_date.Format(time.RFC3339),
		ExpirationDate: newCard.Valid_until.Format(time.RFC3339),
		AccountNumber:  newCard.Account_number,
		Cvv:            newCard.Cvv,
		Limit:          newCard.Card_limit,
		Status:         "Aktivna",
	}, nil
}

func (s *Server) RequestCard(ctx context.Context, req *bankpb.RequestCardRequest) (*bankpb.RequestCardResponse, error) {
	var account Account
	if err := s.db_gorm.Where("number = ?", req.AccountNumber).First(&account).Error; err != nil {
		return nil, status.Error(codes.NotFound, "account not found")
	}

	token, err := generateOpaqueToken()
	if err != nil {
		return nil, status.Error(codes.Internal, "token generation failed")
	}

	cardRequest := CardRequest{
		AccountNumber: req.AccountNumber,
		CardType:      req.CardType,
		Name:          req.Name,
		Limit:         req.Limit,
		TokenHash:     hashValue(token),
		ValidUntil:    time.Now().Add(cardRequestTokenTTL),
	}

	if err := s.db_gorm.Create(&cardRequest).Error; err != nil {
		return nil, status.Error(codes.Internal, "failed to store card request")
	}

	link, err := buildConfirmationLink(token)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to build link")
	}

	// TODO: Izvuci pravi email klijenta iz baze
	if err := s.sendCardConfirmationEmail(ctx, "klijent@email.com", link); err != nil {
		return nil, status.Error(codes.Internal, "failed to send confirmation email")
	}

	return &bankpb.RequestCardResponse{Accepted: true}, nil
}

func (s *Server) ConfirmCard(ctx context.Context, req *bankpb.ConfirmCardRequest) (*bankpb.CardResponse, error) {
	var cardReq CardRequest
	tokenHash := hashValue(req.Token)

	err := s.db_gorm.Where("token_hash = ? AND valid_until > ? AND confirmed = ?",
		tokenHash, time.Now(), false).First(&cardReq).Error

	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid or expired token")
	}

	resp, err := s.CreateCard(ctx, &bankpb.CreateCardRequest{
		AccountNumber: cardReq.AccountNumber,
		CardType:      cardReq.CardType,
		Name:          cardReq.Name,
		Limit:         cardReq.Limit,
	})
	if err != nil {
		return nil, err
	}

	s.db_gorm.Model(&cardReq).Update("confirmed", true)
	return resp, nil
}

func (s *Server) checkCardLimits(acc *Account, req *bankpb.CreateCardRequest) error {
	var count int64
	if acc.Owner_type == Personal {
		s.db_gorm.Model(&Card{}).Where("account_number = ?", acc.Number).Count(&count)
		if count >= 2 {
			return status.Error(codes.ResourceExhausted, "personal account can have a maximum of 2 cards")
		}
	} else if acc.Owner_type == Business {
		s.db_gorm.Model(&Card{}).Where("account_number = ? AND name = ?", acc.Number, req.Name).Count(&count)
		if count >= 1 {
			return status.Error(codes.ResourceExhausted, "this person already has a card for this business account")
		}
	}
	return nil
}

func (s *Server) sendCardConfirmationEmail(ctx context.Context, email string, link string) error {
	addr := os.Getenv("NOTIFICATION_GRPC_ADDR")
	if addr == "" {
		addr = defaultNotificationURL
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	client := notificationpb.NewNotificationServiceClient(conn)
	_, err = client.SendCardConfirmationEmail(ctx, &notificationpb.CardConfirmationMailRequest{
		ToAddr: email,
		Link:   link,
	})
	return err
}

func mapCompanyToProto(company *Company) *userpb.Company {
	if company == nil {
		return nil
	}
	return &userpb.Company{
		Id:             company.Id,
		RegisteredId:   company.Registered_id,
		Name:           company.Name,
		TaxCode:        company.Tax_code,
		ActivityCodeId: company.Activity_code_id,
		Address:        company.Address,
		OwnerId:        company.Owner_id,
	}
}

func validateCreateCompanyInput(registeredID int64, name string, taxCode int64, address string, ownerID int64) error {
	if registeredID <= 0 {
		return status.Error(codes.InvalidArgument, "registered id must be greater than zero")
	}
	if strings.TrimSpace(name) == "" {
		return status.Error(codes.InvalidArgument, "name is required")
	}
	if taxCode <= 0 {
		return status.Error(codes.InvalidArgument, "tax code must be greater than zero")
	}
	if strings.TrimSpace(address) == "" {
		return status.Error(codes.InvalidArgument, "address is required")
	}
	if ownerID <= 0 {
		return status.Error(codes.InvalidArgument, "owner id must be greater than zero")
	}
	return nil
}

func validateUpdateCompanyInput(id int64, name string, address string, ownerID int64) error {
	if id <= 0 {
		return status.Error(codes.InvalidArgument, "id must be greater than zero")
	}
	if strings.TrimSpace(name) == "" {
		return status.Error(codes.InvalidArgument, "name is required")
	}
	if strings.TrimSpace(address) == "" {
		return status.Error(codes.InvalidArgument, "address is required")
	}
	if ownerID <= 0 {
		return status.Error(codes.InvalidArgument, "owner id must be greater than zero")
	}
	return nil
}

func (s *Server) CreateCompany(req *userpb.CreateCompanyRequest) (*userpb.CreateCompanyResponse, error) {
	if err := validateCreateCompanyInput(req.RegisteredId, req.Name, req.TaxCode, req.Address, req.OwnerId); err != nil {
		return nil, err
	}

	company, err := s.CreateCompanyRecord(Company{
		Registered_id:    req.RegisteredId,
		Name:             strings.TrimSpace(req.Name),
		Tax_code:         req.TaxCode,
		Activity_code_id: req.ActivityCodeId,
		Address:          strings.TrimSpace(req.Address),
		Owner_id:         req.OwnerId,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrCompanyRegisteredIDExists):
			return nil, status.Error(codes.AlreadyExists, "company with that registered id already exists")
		case errors.Is(err, ErrCompanyOwnerNotFound):
			return nil, status.Error(codes.InvalidArgument, "owner does not exist")
		case errors.Is(err, ErrCompanyActivityCodeNotFound):
			return nil, status.Error(codes.InvalidArgument, "activity code does not exist")
		default:
			return nil, status.Error(codes.Internal, "company creation failed")
		}
	}

	return &userpb.CreateCompanyResponse{Company: mapCompanyToProto(company)}, nil
}

func (s *Server) GetCompanyById(req *userpb.GetCompanyByIdRequest) (*userpb.GetCompanyByIdResponse, error) {
	if req.Id <= 0 {
		return nil, status.Error(codes.InvalidArgument, "id must be greater than zero")
	}

	company, err := s.GetCompanyByIDRecord(req.Id)
	if err != nil {
		switch {
		case errors.Is(err, ErrCompanyNotFound):
			return nil, status.Error(codes.NotFound, "company not found")
		default:
			return nil, status.Error(codes.Internal, "company lookup failed")
		}
	}

	return &userpb.GetCompanyByIdResponse{Company: mapCompanyToProto(company)}, nil
}

func (s *Server) GetCompanies() (*userpb.GetCompaniesResponse, error) {
	companies, err := s.GetCompaniesRecords()
	if err != nil {
		return nil, status.Error(codes.Internal, "company listing failed")
	}

	var responseCompanies []*userpb.Company
	for _, company := range companies {
		responseCompanies = append(responseCompanies, mapCompanyToProto(company))
	}

	return &userpb.GetCompaniesResponse{Companies: responseCompanies}, nil
}

func (s *Server) UpdateCompany(req *userpb.UpdateCompanyRequest) (*userpb.UpdateCompanyResponse, error) {
	if err := validateUpdateCompanyInput(req.Id, req.Name, req.Address, req.OwnerId); err != nil {
		return nil, err
	}

	company, err := s.UpdateCompanyRecord(Company{
		Id:               req.Id,
		Name:             strings.TrimSpace(req.Name),
		Activity_code_id: req.ActivityCodeId,
		Address:          strings.TrimSpace(req.Address),
		Owner_id:         req.OwnerId,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrCompanyNotFound):
			return nil, status.Error(codes.NotFound, "company not found")
		case errors.Is(err, ErrCompanyOwnerNotFound):
			return nil, status.Error(codes.InvalidArgument, "owner does not exist")
		case errors.Is(err, ErrCompanyActivityCodeNotFound):
			return nil, status.Error(codes.InvalidArgument, "activity code does not exist")
		default:
			return nil, status.Error(codes.Internal, "company update failed")
		}
	}

	return &userpb.UpdateCompanyResponse{Company: mapCompanyToProto(company)}, nil
}
