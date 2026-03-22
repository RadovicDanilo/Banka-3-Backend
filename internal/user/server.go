package user

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"time"

	"gorm.io/gorm"

	userpb "github.com/RAF-SI-2025/Banka-3-Backend/gen/user"
)

const (
	passwordActionReset      = "reset"
	passwordActionInitialSet = "initial_set"

	resetPasswordTokenTTL  = 30 * time.Minute
	initialSetPasswordTTL  = 24 * time.Hour
	defaultNotificationURL = "notification:50051"
)

type Server struct {
	userpb.UnimplementedUserServiceServer
	accessJwtSecret  string
	refreshJwtSecret string
	database         *sql.DB
	db_gorm          *gorm.DB
}

func NewServer(accessJwtSecret string, refreshJwtSecret string, database *sql.DB, gorm_db *gorm.DB) *Server {
	return &Server{
		accessJwtSecret:  accessJwtSecret,
		refreshJwtSecret: refreshJwtSecret,
		database:         database,
		db_gorm:          gorm_db,
	}
}

func generateSalt() ([]byte, error) {
	salt := make([]byte, 16)
	_, err := rand.Read(salt)
	if err != nil {
		return nil, err
	}
	return salt, nil
}

func HashPassword(password string, salt []byte) []byte {
	hashed := sha256.New()
	hashed.Write(salt)
	hashed.Write([]byte(password))
	return hashed.Sum(nil)
}

func hashValue(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}
