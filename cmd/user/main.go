package main

import (
	"banka-raf/gen/notification"
	"banka-raf/gen/user"
	internalUser "banka-raf/internal/user"
	"context"
	"fmt"
	"log"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

func main() {
	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50051"
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	srv := grpc.NewServer()
	user.RegisterUserServiceServer(srv, &internalUser.Server{})
	reflection.Register(srv)

	log.Printf("user service listening on :%s", port)
	conn, err := grpc.Dial(
		"notification:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatal(err)
	}

	client := notification.NewNotificationServiceClient(conn)

	resp, err := client.SendActivationEmail(context.Background(), &notification.ActivationMailRequest{
		ToAddr: "pajicaleksa.12@gmail.com",
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("resp: %s", resp)

	if err := srv.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}

}
