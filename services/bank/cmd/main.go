package main

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/RAF-SI-2025/Banka-3-Backend/pkg/logger"
	"github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto/bank"
	internalBank "github.com/RAF-SI-2025/Banka-3-Backend/services/bank/internal/bank"
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// dsnWithExecMode forces pgx's `default_query_exec_mode=exec` unless the
// caller has already pinned it. This sidesteps the "cached plan must not
// change result type" failure that hits when migrations land *after* the
// service connects (typical: docker compose up → schema.sql → seed.sql,
// where the connection pool predates the schema). `exec` skips the per-
// statement describe-cache without dropping prepared statements wholesale.
func dsnWithExecMode(dsn string) string {
	if strings.Contains(dsn, "default_query_exec_mode") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "default_query_exec_mode=exec"
}

func connect_to_db_gorm() *gorm.DB {
	dsn := dsnWithExecMode(os.Getenv("DATABASE_URL"))
	gorm_db, gorm_err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if gorm_err != nil {
		logger.L().Error("gorm open failed", "err", gorm_err)
		os.Exit(1)
	}
	return gorm_db
}

func connectToDB() *sql.DB {
	connStr := dsnWithExecMode(os.Getenv("DATABASE_URL"))
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		logger.L().Error("sql open failed", "err", err)
		os.Exit(1)
	}
	return db
}

func main() {
	logger.Init("bank")

	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50051"
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		logger.L().Error("failed to listen", "port", port, "err", err)
		os.Exit(1)
	}

	db := connectToDB()
	gorm_db := connect_to_db_gorm()
	logger.L().Info("connected to database")
	defer func() { _ = db.Close() }()

	bankService, err := internalBank.NewServer(db, gorm_db)
	if err != nil {
		logger.L().Error("failed to start bank service", "err", err)
		os.Exit(1)
	}
	stopScheduler := bankService.StartScheduler()
	defer stopScheduler()

	srv := grpc.NewServer(
		grpc.UnaryInterceptor(logger.UnaryServerInterceptor()),
		grpc.StreamInterceptor(logger.StreamServerInterceptor()),
	)
	bank.RegisterBankServiceServer(srv, bankService)
	reflection.Register(srv)

	logger.L().Info("bank service listening", "port", port)
	if err := srv.Serve(lis); err != nil {
		logger.L().Error("failed to serve", "err", err)
		os.Exit(1)
	}
}
