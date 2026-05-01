package main

import (
	"database/sql"
	"fmt"
	"net"
	"os"

	"github.com/RAF-SI-2025/Banka-3-Backend/pkg/logger"
	tradingpb "github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto/trading"
	"github.com/RAF-SI-2025/Banka-3-Backend/services/trading/internal/trading"
	"github.com/RAF-SI-2025/Banka-3-Backend/services/trading/internal/trading/pricing"
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
		logger.L().Error("gorm open failed", "err", gorm_err)
		os.Exit(1)
	}
	return gorm_db
}

func connectToDB() *sql.DB {
	connStr := os.Getenv("DATABASE_URL")
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		logger.L().Error("sql open failed", "err", err)
		os.Exit(1)
	}
	return db
}

// buildPricingClient assembles the external-pricing client from env vars.
// Returns nil when no provider is configured — Refresher.Start treats that
// as a no-op, so dev/CI runs without API keys keep working off the static
// seed prices from #195. Order matters: Alpaca is tried first because its
// quote endpoint exposes ask/bid (which AV's free tier doesn't); AV serves
// as a fallback for tickers Alpaca refuses.
func buildPricingClient() pricing.Client {
	var clients []pricing.Client
	if id, secret := os.Getenv("ALPACA_KEY_ID"), os.Getenv("ALPACA_SECRET"); id != "" && secret != "" {
		clients = append(clients, pricing.NewAlpaca(id, secret))
	}
	if key := os.Getenv("ALPHAVANTAGE_KEY"); key != "" {
		clients = append(clients, pricing.NewAlphaVantage(key))
	}
	if len(clients) == 0 {
		return nil
	}
	return pricing.NewMulti(clients...)
}

// buildDailyHistoryClient is AV-only: TIME_SERIES_DAILY is the spec-named
// daily-history source (p.40) and Alpaca's bars endpoint is out of scope for
// #228. nil here disables the backfiller, same convention as the refresher.
func buildDailyHistoryClient() trading.DailyHistoryClient {
	if key := os.Getenv("ALPHAVANTAGE_KEY"); key != "" {
		return pricing.NewAlphaVantage(key)
	}
	return nil
}

// buildCompanyOverviewClient is AV-only as well: OVERVIEW has no Alpaca
// equivalent and the spec table on p.40 names AV as the source for the
// metadata fields the syncer touches (#229).
func buildCompanyOverviewClient() trading.CompanyOverviewClient {
	if key := os.Getenv("ALPHAVANTAGE_KEY"); key != "" {
		return pricing.NewAlphaVantage(key)
	}
	return nil
}

// buildOptionsChainClient wires Yahoo Finance's options endpoint (#230).
// Yahoo's public endpoint requires no API key, so we expose a single boolean
// gate (TRADING_OPTIONS_REFRESH=1) to opt in — defaulting off so dev/CI runs
// don't make outbound calls every hour.
func buildOptionsChainClient() trading.OptionsChainClient {
	if os.Getenv("TRADING_OPTIONS_REFRESH") == "1" {
		return pricing.NewYahoo()
	}
	return nil
}

// buildForexRatesClient wires exchangerate-api (#230). Open endpoint is
// keyless; EXCHANGERATE_KEY routes to the paid host with the same response
// shape. nil disables the forex refresher.
func buildForexRatesClient() trading.ForexRatesClient {
	if os.Getenv("TRADING_FOREX_REFRESH") == "1" || os.Getenv("EXCHANGERATE_KEY") != "" {
		return pricing.NewExchangeRate(os.Getenv("EXCHANGERATE_KEY"))
	}
	return nil
}

func main() {
	logger.Init("trading")

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

	tradingService, _ := trading.NewServer(gorm_db)
	stopExecutor := tradingService.StartExecutor()
	defer stopExecutor()

	stopScheduler := tradingService.StartScheduler()
	defer stopScheduler()

	// External-pricing refresher (#184). No-op when no API keys are
	// configured, so dev/CI keep operating off the static seed data from
	// #195.
	stopRefresher := trading.NewRefresher(gorm_db, buildPricingClient()).Start()
	defer stopRefresher()

	// Daily-history backfiller (#228). No-op without ALPHAVANTAGE_KEY.
	stopBackfiller := trading.NewBackfiller(gorm_db, buildDailyHistoryClient()).Start()
	defer stopBackfiller()

	// Stock metadata syncer (#229). Weekly OVERVIEW pull. No-op without
	// ALPHAVANTAGE_KEY.
	stopMetadata := trading.NewMetadataSyncer(gorm_db, buildCompanyOverviewClient()).Start()
	defer stopMetadata()

	// Options-chain refresher (#230). Yahoo Finance, opt-in via
	// TRADING_OPTIONS_REFRESH=1.
	stopOptions := trading.NewOptionsRefresher(gorm_db, buildOptionsChainClient()).Start()
	defer stopOptions()

	// Forex-rates refresher (#230). exchangerate-api, opt-in via
	// TRADING_FOREX_REFRESH=1 or by setting EXCHANGERATE_KEY.
	stopForex := trading.NewForexRefresher(gorm_db, buildForexRatesClient()).Start()
	defer stopForex()

	srv := grpc.NewServer(
		grpc.UnaryInterceptor(logger.UnaryServerInterceptor()),
		grpc.StreamInterceptor(logger.StreamServerInterceptor()),
	)
	tradingpb.RegisterTradingServiceServer(srv, tradingService)
	reflection.Register(srv)

	logger.L().Info("trading service listening", "port", port)
	if err := srv.Serve(lis); err != nil {
		logger.L().Error("failed to serve", "err", err)
		os.Exit(1)
	}
}
