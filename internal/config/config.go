package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"go.uber.org/fx"
)

const defaultDatabaseURL = "postgres://ncode:ncode_local_dev@127.0.0.1:54329/ncode?sslmode=disable"

type Config struct {
	Address     string
	DatabaseURL string
	AppEnv      string
	SeedDemo    bool
}

var Module = fx.Module("config", fx.Provide(Load))

func Load() (Config, error) {
	address := strings.TrimSpace(os.Getenv("API_ADDR"))
	if address == "" {
		address = "127.0.0.1:8080"
	}
	host, _, err := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	if err != nil || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return Config{}, fmt.Errorf("API_ADDR must bind to a loopback address")
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		databaseURL = defaultDatabaseURL
	}

	appEnv := strings.TrimSpace(os.Getenv("APP_ENV"))
	if appEnv == "" {
		appEnv = "development"
	}
	seedDemo := appEnv == "development"
	if raw := strings.TrimSpace(os.Getenv("SEED_DEMO")); raw != "" {
		seedDemo, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("SEED_DEMO must be a boolean: %w", err)
		}
	}
	if appEnv == "production" {
		seedDemo = false
	}

	return Config{
		Address:     address,
		DatabaseURL: databaseURL,
		AppEnv:      appEnv,
		SeedDemo:    seedDemo,
	}, nil
}
