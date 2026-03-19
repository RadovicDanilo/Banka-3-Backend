package bank

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	bankpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/bank"
	userpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/user"
)

const (
	defaultNotificationURL = "notification:50051"
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

func validateCreateCardInput(req *bankpb.CreateCardRequest) error {
	if strings.TrimSpace(req.AccountNumber) == "" {
		return status.Error(codes.InvalidArgument, "account number is required")
	}
	if strings.TrimSpace(req.CardType) == "" {
		return status.Error(codes.InvalidArgument, "card type is required")
	}
	if strings.TrimSpace(req.Name) == "" {
		return status.Error(codes.InvalidArgument, "card name is required")
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

	if account.Owner_type == Personal {
		var count int64
		s.db_gorm.Model(&Card{}).Where("account_number = ?", account.Number).Count(&count)
		if count >= 2 {
			return nil, status.Error(codes.ResourceExhausted, "personal account can have a maximum of 2 cards")
		}
	} else if account.Owner_type == Business {
		var authParty AuthorizedParty
		if err := s.db_gorm.Where("id = ?", req.AuthorizedPartyId).First(&authParty).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, status.Error(codes.NotFound, "authorized party not found")
			}
			return nil, status.Error(codes.Internal, "failed to verify authorized party")
		}

		var count int64
		fullName := authParty.Name + " " + authParty.Last_name
		s.db_gorm.Model(&Card{}).Where("account_number = ? AND name = ?", account.Number, fullName).Count(&count)
		if count >= 1 {
			return nil, status.Error(codes.ResourceExhausted, "this person already has a card for this business account")
		}
	}

	cardNum, err := GenerateCardNumber(req.CardType, account.Number)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate card number")
	}

	cvv := GenerateCVV()
	now := time.Now()
	expiry := now.AddDate(5, 0, 0)

	newCard := Card{
		Number:         cardNum,
		Type:           card_type(req.CardType),
		Name:           req.Name,
		Creation_date:  now,
		Valid_until:    expiry,
		Account_number: account.Number,
		Cvv:            cvv,
		Card_limit:     req.Limit,
		Status:         Active,
	}

	err = s.db_gorm.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&newCard).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		switch {
		case strings.Contains(err.Error(), "duplicate key"):
			return nil, status.Error(codes.AlreadyExists, "card number already exists")
		default:
			return nil, status.Error(codes.Internal, "card creation failed in database")
		}
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
