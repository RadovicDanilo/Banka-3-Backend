package notification

import (
	"banka-raf/gen/notification"
	"bytes"
	"context"
	"fmt"

	//"github.com/joho/godotenv"
	"html/template"
	"log"
	//"net/http"
	"net/smtp" // protocol for sending mails
	"os"
	"strings"
)

type Server struct {
	notification.UnimplementedNotificationServiceServer
}

func (s *Server) SendConfirmationEmail(ctx context.Context, req *notification.ConfirmationMailRequest) (*notification.SuccessResponse, error) {
	//todo implement logic for sending an email

	return &notification.SuccessResponse{
		Successful: true,
	}, nil

}

func (s *Server) SendActivationEmail(ctx context.Context, req *notification.ActivationMailRequest) (*notification.SuccessResponse, error) {
	log.Println("Sending activation email")
	wd, _ := os.Getwd()
	fmt.Println("Working directory:", wd)
	//list of email we want to send to
	to := strings.Split(req.ToAddr, ",")
	templ, err := template.ParseFiles("/internal/notification/templates/activation")
	if err != nil {
		log.Println("Cannot parse activation.html:", err)
		return &notification.SuccessResponse{Successful: false}, nil
	}

	//render the html template
	var rendered bytes.Buffer
	if err := templ.Execute(&rendered, req); err != nil {
		log.Println("Cannot execute activation.html:", err)
		return &notification.SuccessResponse{Successful: false}, nil
	}

	err = sendHTMLEmail(to, "Activate Banka 3 account", rendered.String())
	if err != nil {
		log.Println("Couldn't send email:", err)
		return &notification.SuccessResponse{Successful: false}, nil
	}
	//if mail was sent
	return &notification.SuccessResponse{
		Successful: true,
	}, nil
}
func sendHTMLEmail(to []string, subject string, htmlBody string) error {

	auth := smtp.PlainAuth(
		"",
		os.Getenv("FROM_EMAIL"),
		os.Getenv("FROM_EMAIL_PASSWORD"),
		os.Getenv("FROM_EMAIL_SMTP"),
	)
	log.Printf(os.Getenv("FROM_EMAIL_SMTP"), os.Getenv("FROM_EMAIL_PASSWORD"), os.Getenv("FROM_EMAIL"))

	headers := []string{
		"From: " + os.Getenv("FROM_EMAIL"),
		"To: " + strings.Join(to, ","),
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=\"UTF-8\"",
	}

	message := strings.Join(headers, "\r\n") + "\r\n\r\n" + htmlBody

	return smtp.SendMail(
		os.Getenv("SMTP_ADDR"),
		auth,
		os.Getenv("FROM_EMAIL"),
		to,
		[]byte(message),
	)
}
