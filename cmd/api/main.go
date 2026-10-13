package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/cng1985/ncode/internal/api"
	"github.com/cng1985/ncode/internal/model"
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type config struct {
	Address     string
	DatabaseURL string
	AppEnv      string
	SeedDemo    bool
}

func loadConfig() (config, error) {
	address := os.Getenv("API_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || (host != "localhost" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback())) {
		return config{}, fmt.Errorf("API_ADDR must bind to a loopback address")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://ncode:ncode_local_dev@127.0.0.1:54329/ncode?sslmode=disable"
	}
	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "development"
	}
	seedDemo := appEnv != "production" && os.Getenv("SEED_DEMO") != "false"
	return config{Address: address, DatabaseURL: databaseURL, AppEnv: appEnv, SeedDemo: seedDemo}, nil
}

func openDatabase(cfg config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{Logger: logger.Default.LogMode(logger.Error)})
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return db, nil
}

func bootstrap(lc fx.Lifecycle, db *gorm.DB, cfg config) {
	if err := db.AutoMigrate(&model.Project{}, &model.WorkItem{}, &model.AcceptanceCriterion{}, &model.Activity{}, &model.IdempotencyRecord{}); err != nil {
		log.Fatalf("database migration failed: %v", err)
	}
	if cfg.SeedDemo {
		if err := model.SeedDemo(db); err != nil {
			log.Fatalf("demo seed failed: %v", err)
		}
	}
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           api.NewRouter(db),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				log.Printf("NCode API listening on %s", cfg.Address)
				if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Printf("API server stopped: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error { return server.Shutdown(ctx) },
	})
}

func main() {
	fx.New(
		fx.Provide(loadConfig),
		fx.Provide(func(cfg config) (*gorm.DB, error) { return openDatabase(cfg) }),
		fx.Invoke(bootstrap),
	).Run()
}
