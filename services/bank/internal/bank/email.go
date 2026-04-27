package bank

import (
	"context"
	"fmt"

	notificationpb "github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto/notification"

	"github.com/RAF-SI-2025/Banka-3-Backend/pkg/logger"
)

func (s *Server) sendCardCreatedEmail(ctx context.Context, email string) error {
	l := logger.FromContext(ctx).With("notification", "CardCreated", "to", email)
	l.InfoContext(ctx, "sending notification email")

	_, err := s.NotificationService.SendCardCreatedEmail(ctx, &notificationpb.CardCreatedMailRequest{
		ToAddr: email,
	})
	if err != nil {
		l.ErrorContext(ctx, "SendCardCreatedEmail failed", "err", err)
		return err
	}

	l.InfoContext(ctx, "notification email sent")
	return nil
}

func (s *Server) sendLoanPaymentFailedEmail(ctx context.Context, email, loanNumber, amount, currency, dueDate string) error {
	l := logger.FromContext(ctx).With("notification", "LoanPaymentFailed", "to", email, "loan", loanNumber)
	l.InfoContext(ctx, "sending notification email")

	_, err := s.NotificationService.SendLoanPaymentFailedEmail(ctx, &notificationpb.LoanPaymentFailedMailRequest{
		ToAddr:     email,
		LoanNumber: loanNumber,
		Amount:     amount,
		Currency:   currency,
		DueDate:    dueDate,
	})
	if err != nil {
		l.ErrorContext(ctx, "SendLoanPaymentFailedEmail failed", "err", err)
		return err
	}

	l.InfoContext(ctx, "notification email sent")
	return nil
}

func (s *Server) sendLoanPaymentSuccessEmail(ctx context.Context, email, loanID, amount, currency string) error {
	l := logger.FromContext(ctx).With("notification", "LoanPaymentSuccess", "to", email, "loan", loanID)
	l.InfoContext(ctx, "sending notification email")

	subject := "Uspešna uplata rate kredita"
	body := fmt.Sprintf("Poštovani,\n\nUspešno je naplaćena rata za kredit #%s u iznosu od %s %s.\n\nHvala na korišćenju naših usluga.\n\nBanka 3", loanID, amount, currency)

	_, err := s.NotificationService.SendConfirmationEmail(ctx, &notificationpb.ConfirmationMailRequest{
		ToAddr:  email,
		Subject: subject,
		Body:    body,
	})
	if err != nil {
		l.ErrorContext(ctx, "SendConfirmationEmail failed", "err", err)
		return err
	}

	l.InfoContext(ctx, "notification email sent")
	return nil
}

func (s *Server) sendCardConfirmationEmail(ctx context.Context, email string, link string) error {
	l := logger.FromContext(ctx).With("notification", "CardConfirmation", "to", email)
	l.InfoContext(ctx, "sending notification email")

	_, err := s.NotificationService.SendCardConfirmationEmail(ctx, &notificationpb.CardConfirmationMailRequest{
		ToAddr: email,
		Link:   link,
	})
	if err != nil {
		l.ErrorContext(ctx, "SendCardConfirmationEmail failed", "err", err)
		return err
	}

	l.InfoContext(ctx, "notification email sent")
	return nil
}

func (s *Server) sendCardBlockedEmail(ctx context.Context, email string, isBlocked bool) error {
	l := logger.FromContext(ctx).With("notification", "CardBlocked", "to", email, "is_blocked", isBlocked)
	l.InfoContext(ctx, "sending notification email")

	_, err := s.NotificationService.SendCardBlockedEmail(ctx, &notificationpb.CardBlockedReqest{
		ToAddr:    email,
		IsBlocked: isBlocked,
	})
	if err != nil {
		l.ErrorContext(ctx, "SendCardBlockedEmail failed", "err", err)
		return err
	}

	l.InfoContext(ctx, "notification email sent")
	return nil
}
