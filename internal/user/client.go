package user

import (
	"context"
	"errors"
	"log"
	"slices"
	"strings"
	"time"

	userpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/user"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func mapClientToProto(client Client) *userpb.Client {
	return &userpb.Client{
		Id:          int64(client.Id),
		FirstName:   client.First_name,
		LastName:    client.Last_name,
		DateOfBirth: client.Date_of_birth.Unix(),
		Gender:      client.Gender,
		Email:       client.Email,
		PhoneNumber: client.Phone_number,
		Address:     client.Address,
	}
}

func (s *Server) CreateClientAccount(_ context.Context, req *userpb.CreateClientRequest) (*userpb.CreateClientResponse, error) {
	is_null := func(str string) bool { return strings.TrimSpace(str) == "" }
	vals := []string{req.FirstName, req.LastName, req.Gender, req.Email, req.PhoneNumber, req.Address}

	if slices.ContainsFunc(vals, is_null) {
		return nil, status.Error(codes.InvalidArgument, "One of the required cols is null")
	}
	if req.Gender != "M" && req.Gender != "F" {
		return nil, status.Error(codes.InvalidArgument, "Gender must be one of M or F")
	}

	salt, salt_err := generateSalt()
	if salt_err != nil {
		log.Printf("Error generating salt %s", salt_err.Error())
		return nil, status.Error(codes.Internal, "Password salting failed")
	}

	client := Client{
		First_name:    req.FirstName,
		Last_name:     req.LastName,
		Date_of_birth: time.Unix(req.BirthDate, 0),
		Gender:        req.Gender,
		Email:         req.Email,
		Phone_number:  req.PhoneNumber,
		Address:       req.Address,
		Password:      HashPassword(req.Password, salt),
		Salt_password: salt,
	}

	err := create_user_from_model(client, s)
	if err != nil {
		log.Printf("Error in user creation %s", err.Error())
		return nil, status.Error(codes.Internal, "Client creation failed")
	}
	return &userpb.CreateClientResponse{Valid: true}, nil
}

func (s *Server) GetClients(_ context.Context, req *userpb.GetClientsRequest) (*userpb.GetClientsResponse, error) {
	clients, err := s.GetAllClients(strings.TrimSpace(req.FirstName), strings.TrimSpace(req.LastName), strings.TrimSpace(req.Email))
	if err != nil {
		log.Printf("Error in retrieving clients: %s", err.Error())
		return nil, status.Error(codes.Internal, "Failed to retrieve clients")
	}

	var clientResponses []*userpb.Client
	for _, client := range clients {
		clientResponses = append(clientResponses, mapClientToProto(client))
	}
	return &userpb.GetClientsResponse{Clients: clientResponses}, nil
}

func (s *Server) UpdateClient(_ context.Context, req *userpb.UpdateClientRequest) (*userpb.UpdateClientResponse, error) {
	if req.Id <= 0 {
		return nil, status.Error(codes.InvalidArgument, "id must be greater than zero")
	}
	if strings.TrimSpace(req.Gender) != "" && req.Gender != "M" && req.Gender != "F" {
		return nil, status.Error(codes.InvalidArgument, "Gender must be one of M or F")
	}

	_, err := s.GetClientByID(req.Id)
	if err != nil {
		switch {
		case errors.Is(err, ErrClientNotFound):
			return nil, status.Error(codes.NotFound, "client not found")
		default:
			return nil, status.Error(codes.Internal, "client lookup failed")
		}
	}

	client := Client{
		Id:           uint64(req.Id),
		First_name:   req.FirstName,
		Last_name:    req.LastName,
		Gender:       req.Gender,
		Email:        req.Email,
		Phone_number: req.PhoneNumber,
		Address:      req.Address,
	}
	if req.DateOfBirth != 0 {
		client.Date_of_birth = time.Unix(req.DateOfBirth, 0)
	}

	err = s.UpdateClientRecord(&client)
	if err != nil {
		switch {
		case errors.Is(err, ErrClientNotFound):
			return nil, status.Error(codes.NotFound, "client not found")
		case errors.Is(err, ErrClientEmailExists):
			return nil, status.Error(codes.AlreadyExists, "client with that email already exists")
		case errors.Is(err, ErrClientNoFieldsToUpdate):
			return nil, status.Error(codes.InvalidArgument, "no fields to update")
		default:
			return nil, status.Error(codes.Internal, "client update failed")
		}
	}
	return &userpb.UpdateClientResponse{Valid: true, Response: "Client updated"}, nil
}
