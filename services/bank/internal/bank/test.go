package bank

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	notificationpb "github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto/notification"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type TestNotificationServer struct {
	notificationpb.UnimplementedNotificationServiceServer
}

func newTestServer(t *testing.T) (*Server, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	server, _ := NewServer(db, nil)
	return server, mock, db
}

func NewGormTestServer(t *testing.T) (*Server, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	server, _ := NewServer(db, gormDB)
	return server, mock, db
}
