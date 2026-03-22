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

func (emp Employee) toProtobuf() *userpb.GetEmployeeResponse {
	permissions := make([]string, len(emp.Permissions))
	for i, v := range emp.Permissions {
		permissions[i] = v.Name
	}
	return &userpb.GetEmployeeResponse{
		Id:          int64(emp.Id),
		FirstName:   emp.First_name,
		LastName:    emp.Last_name,
		BirthDate:   emp.Date_of_birth.Unix(),
		Gender:      emp.Gender,
		Email:       emp.Email,
		PhoneNumber: emp.Phone_number,
		Address:     emp.Address,
		Username:    emp.Username,
		Position:    emp.Position,
		Department:  emp.Department,
		Active:      emp.Active,
		Permissions: permissions,
	}
}

func (s *Server) CreateEmployeeAccount(ctx context.Context, req *userpb.CreateEmployeeRequest) (*userpb.GetEmployeeResponse, error) {
	is_null := func(str string) bool { return strings.TrimSpace(str) == "" }
	vals := []string{req.FirstName, req.LastName, req.Email, req.Username}
	if slices.ContainsFunc(vals, is_null) {
		return nil, status.Error(codes.InvalidArgument, "One of the required cols is null")
	}

	salt, salt_err := generateSalt()
	if salt_err != nil {
		log.Printf("Error generating salt %s", salt_err.Error())
	}

	employee := Employee{
		First_name:    req.FirstName,
		Last_name:     req.LastName,
		Date_of_birth: time.Unix(req.BirthDate, 0),
		Gender:        req.Gender,
		Email:         req.Email,
		Phone_number:  req.PhoneNumber,
		Address:       req.Address,
		Username:      req.Username,
		Position:      req.Position,
		Department:    req.Department,
		Salt_password: salt,
		Password:      []byte{},
	}

	err := create_user_from_model(employee, s)
	if err != nil {
		log.Printf("Error in user creation %s", err.Error())
		return nil, status.Error(codes.Internal, "Employee creation failed")
	}

	_, emailErr := s.RequestInitialPasswordSet(ctx, &userpb.PasswordActionRequest{Email: req.Email})
	if emailErr != nil {
		log.Printf("Employee created but activation email failed: %s", emailErr.Error())
	}

	return employee.toProtobuf(), nil
}

func (s *Server) GetEmployeeByEmail(_ context.Context, req *userpb.GetEmployeeByEmailRequest) (*userpb.GetEmployeeResponse, error) {
	resp, err := s.getEmployeeByEmail(req.Email)
	if err != nil {
		return nil, err
	}
	return resp.toProtobuf(), nil
}

func (s *Server) GetEmployeeById(_ context.Context, req *userpb.GetEmployeeByIdRequest) (*userpb.GetEmployeeResponse, error) {
	resp, err := s.getEmployeeById(req.Id)
	if err != nil {
		return nil, err
	}
	return resp.toProtobuf(), nil
}

func (s *Server) DeleteEmployee(_ context.Context, req *userpb.DeleteEmployeeRequest) (*userpb.DeleteEmployeeResponse, error) {
	err := s.deleteEmployee(req.Id)
	if err != nil {
		if errors.Is(err, ErrEmployeeNotFound) {
			return nil, status.Error(codes.NotFound, "employee not found")
		}
		return nil, err
	}
	return &userpb.DeleteEmployeeResponse{Success: true}, nil
}

func (s *Server) GetEmployees(_ context.Context, req *userpb.GetEmployeesRequest) (*userpb.GetEmployeesResponse, error) {
	map_func := func(emp Employee) *userpb.GetEmployeesResponse_Employee {
		return &userpb.GetEmployeesResponse_Employee{
			Id:          int64(emp.Id),
			FirstName:   emp.First_name,
			LastName:    emp.Last_name,
			Email:       emp.Email,
			Position:    emp.Position,
			PhoneNumber: emp.Phone_number,
			Active:      emp.Active,
		}
	}
	employees, err := s.GetAllEmployees(&req.Email, &req.FirstName, &req.LastName, &req.Position)
	if err != nil {
		log.Printf("Error in retrieving employees: %s", err.Error())
		return nil, status.Error(codes.Internal, "Failed to retrieve employees")
	}
	var employee_responses []*userpb.GetEmployeesResponse_Employee
	for _, emp := range employees {
		employee_responses = append(employee_responses, map_func(emp))
	}
	return &userpb.GetEmployeesResponse{Employees: employee_responses}, nil
}

func (s *Server) UpdateEmployee(_ context.Context, req *userpb.UpdateEmployeeRequest) (*userpb.GetEmployeeResponse, error) {
	var permissions []Permission
	for _, perm := range req.Permissions {
		permissions = append(permissions, Permission{Id: 0, Name: perm})
	}

	emp := Employee{
		First_name:   req.FirstName,
		Last_name:    req.LastName,
		Gender:       req.Gender,
		Phone_number: req.PhoneNumber,
		Address:      req.Address,
		Position:     req.Position,
		Department:   req.Department,
		Active:       req.Active,
		Id:           uint64(req.Id),
		Updated_at:   time.Now(),
		Permissions:  permissions,
	}

	updated, err := s.UpdateEmployee_(&emp)
	if err != nil {
		if errors.Is(err, ErrEmployeeNotFound) {
			return nil, status.Error(codes.NotFound, "Employee not found")
		}
		if errors.Is(err, ErrUnknownPermission) {
			return nil, status.Error(codes.NotFound, "Uknown permissions")
		}
		return nil, status.Error(codes.Internal, "Messed something up in UpdateEmployee_ in repo")
	}
	return updated.toProtobuf(), nil
}
