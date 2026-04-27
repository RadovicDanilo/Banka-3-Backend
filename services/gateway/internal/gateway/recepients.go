package gateway

import (
	"context"
	"net/http"
	"time"

	bankpb "github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto/bank"

	"github.com/gin-gonic/gin"
)

func (s *Server) GetPaymentRecipients(c *gin.Context) {
	clientID, ok := s.getAuthenticatedClientID(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.BankClient.GetPaymentRecipients(ctx, &bankpb.GetPaymentRecipientsRequest{
		ClientId: clientID,
	})
	if err != nil {
		writeGRPCError(c, err)
		return
	}

	recipients := make([]gin.H, 0)

	for _, r := range resp.Recipients {
		recipients = append(recipients, gin.H{
			"id":             r.Id,
			"name":           r.Name,
			"account_number": r.AccountNumber,
		})
	}

	c.JSON(http.StatusOK, recipients)
}

func (s *Server) CreatePaymentRecipient(c *gin.Context) {
	var req createPaymentRecipientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}

	clientID, ok := s.getAuthenticatedClientID(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.BankClient.CreatePaymentRecipient(ctx, &bankpb.CreatePaymentRecipientRequest{
		ClientId:      clientID,
		Name:          req.Name,
		AccountNumber: req.AccountNumber,
	})
	if err != nil {
		writeGRPCError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":             resp.Recipient.Id,
		"name":           resp.Recipient.Name,
		"account_number": resp.Recipient.AccountNumber,
	})
}

func (s *Server) UpdatePaymentRecipient(c *gin.Context) {
	var uri paymentRecipientByIDURI
	if err := c.ShouldBindUri(&uri); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid recipient id",
		})
		return
	}

	var req updatePaymentRecipientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}

	clientID, ok := s.getAuthenticatedClientID(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := s.BankClient.UpdatePaymentRecipient(ctx, &bankpb.UpdatePaymentRecipientRequest{
		Id:            uri.ID,
		ClientId:      clientID,
		Name:          req.Name,
		AccountNumber: req.AccountNumber,
	})
	if err != nil {
		writeGRPCError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Recipient updated"})
}

func (s *Server) DeletePaymentRecipient(c *gin.Context) {
	var uri paymentRecipientByIDURI
	if err := c.ShouldBindUri(&uri); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid recipient id",
		})
		return
	}

	clientID, ok := s.getAuthenticatedClientID(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := s.BankClient.DeletePaymentRecipient(ctx, &bankpb.DeletePaymentRecipientRequest{
		Id:       uri.ID,
		ClientId: clientID,
	})
	if err != nil {
		writeGRPCError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
