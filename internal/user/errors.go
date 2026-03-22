package user

import (
	"errors"
	"log"

	"github.com/jackc/pgx/v5/pgconn"
)

var ErrInvalidPasswordActionToken = errors.New("invalid or expired password token")
var ErrClientNotFound = errors.New("client not found")
var ErrClientEmailExists = errors.New("client email already exists")
var ErrClientNoFieldsToUpdate = errors.New("no client fields to update")
var ErrEmployeeNotFound = errors.New("employee not found")
var ErrUnknownPermission = errors.New("unknown permissions")

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func create_user_from_model[T Client | Employee](user T, s *Server) error {
	result := s.db_gorm.Create(&user)
	if result.Error != nil {
		log.Printf("We got this error: %s", result.Error.Error())
		return result.Error
	}
	return nil
}
