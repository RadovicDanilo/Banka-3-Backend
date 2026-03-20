package bank

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"

	bankpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/bank"
	notificationpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/notification"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

const (
	defaultNotificationURL = "notification:50051"
	defaultBankURL         = "bank:50051"
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

func mapCompanyToProto(company *Company) *bankpb.Company {
	if company == nil {
		return nil
	}

	return &bankpb.Company{
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

func (s *Server) CreateCompany(ctx context.Context, req *bankpb.CreateCompanyRequest) (*bankpb.CreateCompanyResponse, error) {
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

	return &bankpb.CreateCompanyResponse{Company: mapCompanyToProto(company)}, nil
}

func (s *Server) GetCompanyById(ctx context.Context, req *bankpb.GetCompanyByIdRequest) (*bankpb.GetCompanyByIdResponse, error) {
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

	return &bankpb.GetCompanyByIdResponse{Company: mapCompanyToProto(company)}, nil
}

func (s *Server) GetCompanies(ctx context.Context, req *bankpb.GetCompaniesRequest) (*bankpb.GetCompaniesResponse, error) {
	companies, err := s.GetCompaniesRecords()
	if err != nil {
		return nil, status.Error(codes.Internal, "company listing failed")
	}

	var responseCompanies []*bankpb.Company
	for _, company := range companies {
		responseCompanies = append(responseCompanies, mapCompanyToProto(company))
	}

	return &bankpb.GetCompaniesResponse{Companies: responseCompanies}, nil
}

func (s *Server) UpdateCompany(ctx context.Context, req *bankpb.UpdateCompanyRequest) (*bankpb.UpdateCompanyResponse, error) {
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

	return &bankpb.UpdateCompanyResponse{Company: mapCompanyToProto(company)}, nil
}

func (s *Server) checkCardLimits(acc *Account, name string) error {
	var count int64
	if acc.Owner_type == Personal {
		s.db_gorm.Model(&Card{}).Where("account_number = ?", acc.Number).Count(&count)
		if count >= 2 {
			return status.Error(codes.ResourceExhausted, "personal limit reached")
		}
	} else {
		s.db_gorm.Model(&Card{}).Where("account_number = ? AND name = ?", acc.Number, name).Count(&count)
		if count >= 1 {
			return status.Error(codes.ResourceExhausted, "person already has a card for this business account")
		}
	}
	return nil
}

func cardToPb(c *Card) *bankpb.CardResponse {
	return &bankpb.CardResponse{
		CardNumber:     c.Number,
		CardType:       string(c.Type),
		CardName:       c.Name,
		CreationDate:   c.Creation_date.Format(time.RFC3339),
		ExpirationDate: c.Valid_until.Format(time.RFC3339),
		AccountNumber:  c.Account_number,
		Cvv:            c.Cvv,
		Limit:          c.Card_limit,
		Status:         string(c.Status),
	}
}

func generateOpaqueToken() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func hashValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func buildConfirmationLink(token string) (string, error) {
	baseURL := defaultBankURL + "/cards/confirm"

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

func (s *Server) sendCardCreatedEmail(ctx context.Context, email string, cardType string, number string) error {
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
	_, err = client.SendCardCreatedEmail(ctx, &notificationpb.CardCreatedMailRequest{
		ToAddr:     email,
		CardType:   cardType,
		CardNumber: number,
	})
	return err
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

func (s *Server) CreateCard(ctx context.Context, req *bankpb.CreateCardRequest) (*bankpb.CardResponse, error) {
	if err := validateCreateCardInput(req); err != nil {
		return nil, err
	}
	var acc Account
	if err := s.db_gorm.Where("number = ?", req.AccountNumber).First(&acc).Error; err != nil {
		return nil, status.Error(codes.NotFound, "account not found")
	}
	if err := s.checkCardLimits(&acc, req.Name); err != nil {
		return nil, err
	}
	cardNum, _ := GenerateCardNumber(req.CardType, acc.Number)
	card := Card{
		Number:         cardNum,
		Type:           card_type(req.CardType),
		Name:           req.Name,
		Creation_date:  time.Now(),
		Valid_until:    time.Now().AddDate(5, 0, 0),
		Account_number: acc.Number,
		Cvv:            GenerateCVV(),
		Card_limit:     req.Limit,
		Status:         Active,
	}
	if err := s.db_gorm.Create(&card).Error; err != nil {
		return nil, status.Error(codes.Internal, "database error")
	}
	return cardToPb(&card), nil
}

func (s *Server) RequestCard(ctx context.Context, req *bankpb.RequestCardRequest) (*bankpb.RequestCardResponse, error) {
	var acc Account
	if err := s.db_gorm.Where("number = ?", req.AccountNumber).First(&acc).Error; err != nil {
		return nil, status.Error(codes.NotFound, "account not found")
	}
	if acc.Owner_type == Business {
		parts := strings.SplitN(req.Name, " ", 2)
		ln := ""
		if len(parts) > 1 {
			ln = parts[1]
		}
		auth := AuthorizedParty{
			Name:         parts[0],
			Last_name:    ln,
			Email:        req.Email,
			Phone_number: req.Phone,
			Address:      req.Address,
		}
		if err := s.db_gorm.Create(&auth).Error; err != nil {
			return nil, status.Error(codes.Internal, "failed to create authorized party")
		}
	}
	token, err := generateOpaqueToken()
	if err != nil {
		return nil, status.Error(codes.Internal, "token generation failed")
	}
	cardReq := CardRequest{
		AccountNumber:  req.AccountNumber,
		CardType:       req.CardType,
		CardHolderName: req.Name,
		Limit:          req.Limit,
		Email:          req.Email,
		TokenHash:      hashValue(token),
		ValidUntil:     time.Now().Add(cardRequestTokenTTL),
		Confirmed:      false,
	}
	if err := s.db_gorm.Create(&cardReq).Error; err != nil {
		return nil, status.Error(codes.Internal, "failed to store request")
	}
	link, err := buildConfirmationLink(token)
	if err != nil {
		return nil, status.Error(codes.Internal, "link generation failed")
	}
	_ = s.sendCardConfirmationEmail(ctx, req.Email, link)
	return &bankpb.RequestCardResponse{Accepted: true}, nil
}

func (s *Server) ConfirmCard(ctx context.Context, req *bankpb.ConfirmCardRequest) (*bankpb.CardResponse, error) {
	var cardReq CardRequest
	h := hashValue(req.Token)
	if err := s.db_gorm.Where("token_hash = ? AND valid_until > ? AND confirmed = ?", h, time.Now(), false).First(&cardReq).Error; err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid or expired token")
	}
	resp, err := s.CreateCard(ctx, &bankpb.CreateCardRequest{
		AccountNumber: cardReq.AccountNumber,
		CardType:      cardReq.CardType,
		Name:          cardReq.CardHolderName,
		Limit:         cardReq.Limit,
	})
	if err != nil {
		return nil, err
	}
	s.db_gorm.Model(&cardReq).Update("confirmed", true)
	_ = s.sendCardCreatedEmail(ctx, cardReq.Email, resp.CardType, resp.CardNumber)
	return resp, nil
}

func (s *Server) GetCards(ctx context.Context, req *bankpb.GetCardsRequest) (*bankpb.GetCardsResponse, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}
	emails := md.Get("user-email")
	if len(emails) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing identity")
	}
	cards, err := s.GetCardsByEmailRecord(emails[0])
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to fetch cards")
	}
	var pbCards []*bankpb.CardResponse
	for _, c := range cards {
		pbCards = append(pbCards, cardToPb(c))
	}
	return &bankpb.GetCardsResponse{Cards: pbCards}, nil
}

func (s *Server) ToggleCardStatus(ctx context.Context, req *bankpb.ToggleCardStatusRequest) (*bankpb.ToggleCardStatusResponse, error) {
	var card Card
	if err := s.db_gorm.Where("id = ?", req.CardId).First(&card).Error; err != nil {
		return nil, status.Error(codes.NotFound, "card not found")
	}
	st := Blocked
	if req.Active {
		st = Active
	}
	if err := s.db_gorm.Model(&card).Update("status", st).Error; err != nil {
		return nil, status.Error(codes.Internal, "failed to update status")
	}
	return &bankpb.ToggleCardStatusResponse{Success: true}, nil
}
