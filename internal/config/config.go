package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"go.uber.org/fx"
)

type Config struct {
	Address     string
	DatabaseURL string
	AppEnv      string
	SeedDemo    bool
}

type fileConfig struct {
	Server struct {
		Address     string `yaml:"address"`
		Environment string `yaml:"environment"`
		SeedDemo    *bool  `yaml:"seed_demo"`
	} `yaml:"server"`
	Database struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		Name     string `yaml:"name"`
		SSLMode  string `yaml:"ssl_mode"`
	} `yaml:"database"`
}

var Module = fx.Module("config", fx.Provide(Load))

func Load() (Config, error) {
	configPath := strings.TrimSpace(os.Getenv("CONFIG_FILE"))
	if configPath == "" {
		var err error
		configPath, err = findDefaultConfig()
		if err != nil {
			return Config{}, err
		}
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration file %q: %w", configPath, err)
	}
	var file fileConfig
	if err := yaml.UnmarshalWithOptions(raw, &file, yaml.Strict()); err != nil {
		return Config{}, fmt.Errorf("parse configuration file %q: %w", configPath, err)
	}

	address := strings.TrimSpace(file.Server.Address)
	if override := strings.TrimSpace(os.Getenv("API_ADDR")); override != "" {
		address = override
	}
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
		databaseURL, err = file.databaseURL(os.Getenv("DATABASE_PASSWORD"))
		if err != nil {
			return Config{}, fmt.Errorf("invalid database configuration in %q: %w", configPath, err)
		}
	}

	appEnv := strings.TrimSpace(file.Server.Environment)
	if override := strings.TrimSpace(os.Getenv("APP_ENV")); override != "" {
		appEnv = override
	}
	if appEnv == "" {
		appEnv = "development"
	}

	seedDemo := appEnv == "development"
	if file.Server.SeedDemo != nil {
		seedDemo = *file.Server.SeedDemo
	}
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

func (database fileConfig) databaseURL(passwordOverride string) (string, error) {
	host := strings.TrimSpace(database.Database.Host)
	user := strings.TrimSpace(database.Database.User)
	name := strings.TrimSpace(database.Database.Name)
	sslMode := strings.TrimSpace(database.Database.SSLMode)
	port := database.Database.Port

	switch {
	case host == "":
		return "", fmt.Errorf("database.host is required")
	case port < 1 || port > 65535:
		return "", fmt.Errorf("database.port must be between 1 and 65535")
	case user == "":
		return "", fmt.Errorf("database.user is required")
	case name == "":
		return "", fmt.Errorf("database.name is required")
	case sslMode == "":
		return "", fmt.Errorf("database.ssl_mode is required")
	}

	password := database.Database.Password
	if passwordOverride != "" {
		password = passwordOverride
	}
	dsn := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   name,
	}
	query := dsn.Query()
	query.Set("sslmode", sslMode)
	dsn.RawQuery = query.Encode()
	return dsn.String(), nil
}

func findDefaultConfig() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("find configuration file: %w", err)
	}
	for {
		candidate := filepath.Join(dir, "configs", "config.yaml")
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect configuration file %q: %w", candidate, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("configs/config.yaml not found from the current directory; set CONFIG_FILE")
}
