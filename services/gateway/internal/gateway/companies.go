package gateway

import (
	"context"
	"net/http"
	"time"

	bankpb "github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto/bank"
	"github.com/gin-gonic/gin"
)

func companyResponse(company *bankpb.Company) gin.H {
	return gin.H{
		"id":               company.Id,
		"registered_id":    company.RegisteredId,
		"name":             company.Name,
		"tax_code":         company.TaxCode,
		"activity_code_id": company.ActivityCodeId,
		"address":          company.Address,
		"owner_id":         company.OwnerId,
	}
}

func (s *Server) CreateCompany(c *gin.Context) {
	var req createCompanyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.BankClient.CreateCompany(ctx, &bankpb.CreateCompanyRequest{
		RegisteredId:   req.RegisteredID,
		Name:           req.Name,
		TaxCode:        req.TaxCode,
		ActivityCodeId: req.ActivityCodeID,
		Address:        req.Address,
		OwnerId:        req.OwnerID,
	})
	if err != nil {
		writeGRPCError(c, err)
		return
	}

	c.JSON(http.StatusCreated, companyResponse(resp.Company))
}

func (s *Server) GetCompanyByID(c *gin.Context) {
	var uri companyByIDURI
	if err := c.ShouldBindUri(&uri); err != nil {
		c.String(http.StatusBadRequest, "company id is required and must be a valid integer")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.BankClient.GetCompanyById(ctx, &bankpb.GetCompanyByIdRequest{
		Id: uri.CompanyID,
	})
	if err != nil {
		writeGRPCError(c, err)
		return
	}

	c.JSON(http.StatusOK, companyResponse(resp.Company))
}

func (s *Server) GetCompanies(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.BankClient.GetCompanies(ctx, &bankpb.GetCompaniesRequest{})
	if err != nil {
		writeGRPCError(c, err)
		return
	}

	companies := make([]gin.H, 0)
	for _, company := range resp.Companies {
		companies = append(companies, companyResponse(company))
	}

	c.JSON(http.StatusOK, companies)
}

func (s *Server) UpdateCompany(c *gin.Context) {
	var uri companyByIDURI
	if err := c.ShouldBindUri(&uri); err != nil {
		c.String(http.StatusBadRequest, "company id is required and must be a valid integer")
		return
	}

	var req updateCompanyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.BankClient.UpdateCompany(ctx, &bankpb.UpdateCompanyRequest{
		Id:             uri.CompanyID,
		Name:           req.Name,
		ActivityCodeId: req.ActivityCodeID,
		Address:        req.Address,
		OwnerId:        req.OwnerID,
	})
	if err != nil {
		writeGRPCError(c, err)
		return
	}

	c.JSON(http.StatusOK, companyResponse(resp.Company))
}
