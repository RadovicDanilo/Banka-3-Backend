package notification

import (
	"bytes"
	"context"
	"html/template"
	"log"
	"net/smtp"
	"os"
	"strings"

	"github.com/RAF-SI-2025/Banka-3-Backend/gen/notification"
)

type EmailSender interface {
	Send(to []string, subject string, body string) error
}

type SMTPSender struct{}

func (s *SMTPSender) Send(to []string, subject string, body string) error {
	auth := smtp.PlainAuth(
		"",
		os.Getenv("FROM_EMAIL_AUTH"),
		os.Getenv("FROM_EMAIL_PASSWORD"),
		os.Getenv("FROM_EMAIL_SMTP"),
	)
	headers := []string{
		"From: " + os.Getenv("FROM_EMAIL"),
		"To: " + strings.Join(to, ","),
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=\"UTF-8\"",
	}
	message := strings.Join(headers, "\r\n") + "\r\n" + body
	return smtp.SendMail(os.Getenv("SMTP_ADDR"), auth, os.Getenv("FROM_EMAIL"), to, []byte(message))
}

type Server struct {
	notification.UnimplementedNotificationServiceServer
	sender EmailSender
}

func NewServer(sender EmailSender) *Server {
	return &Server{sender: sender}
}

func (s *Server) sendEmail(toAddr string, subject string, templateFile string, data interface{}) (*notification.SuccessResponse, error) {
	to := strings.Split(toAddr, ",")

	templ, err := template.ParseFiles("templates/" + templateFile)
	if err != nil {
		log.Printf("Cannot parse %s: %v\n", templateFile, err)
		return &notification.SuccessResponse{Successful: false}, nil
	}

	var rendered bytes.Buffer
	if err := templ.Execute(&rendered, data); err != nil {
		log.Printf("Cannot execute %s: %v\n", templateFile, err)
		return &notification.SuccessResponse{Successful: false}, nil
	}

	err = s.sender.Send(to, subject, rendered.String())
	if err != nil {
		log.Printf("Couldn't send email (%s): %v\n", templateFile, err)
		return &notification.SuccessResponse{Successful: false}, nil
	}

	return &notification.SuccessResponse{Successful: true}, nil
}

func (s *Server) SendConfirmationEmail(_ context.Context, req *notification.ConfirmationMailRequest) (*notification.SuccessResponse, error) {
	return s.sendEmail(req.ToAddr, "Confirm your Banka 3 account", "confirmation.html", req)
}

func (s *Server) SendActivationEmail(_ context.Context, req *notification.ActivationMailRequest) (*notification.SuccessResponse, error) {
	return s.sendEmail(req.ToAddr, "Aktivirajte Banka 3 nalog", "activation.html", req)
}

func (s *Server) SendPasswordResetEmail(_ context.Context, req *notification.PasswordLinkMailRequest) (*notification.SuccessResponse, error) {
	return s.sendEmail(req.ToAddr, "Reset your Banka 3 password", "password_reset.html", req)
}

func (s *Server) SendInitialPasswordSetEmail(_ context.Context, req *notification.PasswordLinkMailRequest) (*notification.SuccessResponse, error) {
	return s.sendEmail(req.ToAddr, "Set your Banka 3 password", "initial_password_set.html", req)
}

func (s *Server) SendCardConfirmationEmail(_ context.Context, req *notification.CardConfirmationMailRequest) (*notification.SuccessResponse, error) {
	data := struct{ Link string }{Link: req.Link}
	return s.sendEmail(req.ToAddr, "Potvrda zahteva za karticu - Banka 3", "card_confirmation.html", data)
}

func (s *Server) SendCardCreatedEmail(_ context.Context, req *notification.CardCreatedMailRequest) (*notification.SuccessResponse, error) {
	return s.sendEmail(req.ToAddr, "Vaša Banka 3 kartica je spremna!", "card_created.html", req)
}
