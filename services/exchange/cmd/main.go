package main

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto/exchange"
	internalExchange "github.com/RAF-SI-2025/Banka-3-Backend/services/exchange/internal/exchange"
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func connect_to_db_gorm() *gorm.DB {
	dsn := os.Getenv("DATABASE_URL")
	gorm_db, gorm_err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if gorm_err != nil {
		log.Fatal("pgx", dsn)
	}
	return gorm_db
}

func connectToDB() *sql.DB {
	connStr := os.Getenv("DATABASE_URL")
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		log.Fatal(err)
	}
	return db
}

func main() {
	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50051"
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	db := connectToDB()
	gorm_db := connect_to_db_gorm()
	//gorm_db.AutoMigrate(&internalUser.Clients{}, &internalUser.Employees{});
	log.Println("connected to database...")
	defer func() { _ = db.Close() }()

	exchangeService := internalExchange.NewServer(gorm_db)

	srv := grpc.NewServer()
	exchange.RegisterExchangeServiceServer(srv, exchangeService)
	reflection.Register(srv)

	log.Printf("exchange service listening on :%s", port)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
