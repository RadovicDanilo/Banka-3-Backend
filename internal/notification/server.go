package notification

import (
	"banka-raf/gen/notification"
	"context"
)

type Server struct {
	notification.UnimplementedNotificationServiceServer
}

func (s *Server) SendEmail(ctx context.Context, req *notification.ConfirmationMailRequest)(*notification.SuccessResponse, error) {
	//email := req.Email

	//todo implement logic for sending an email

	return &notification.SuccessResponse{
		Successful: true,
	}, nil

}
