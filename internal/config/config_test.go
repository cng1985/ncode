package config

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsDatabaseSettingsFromYAML(t *testing.T) {
	resetEnvironment(t)
	t.Setenv("CONFIG_FILE", writeConfig(t, validConfigLines()...))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Address != "127.0.0.1:8080" || cfg.AppEnv != "development" || !cfg.SeedDemo {
		t.Fatalf("loaded server configuration = %+v", cfg)
	}

	dsn, err := url.Parse(cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	if dsn.Host != "db.example:5432" || dsn.Path != "/ncode_test" || dsn.Query().Get("sslmode") != "require" {
		t.Fatalf("database URL target = %q", cfg.DatabaseURL)
	}
	if username := dsn.User.Username(); username != "ncode_app" {
		t.Fatalf("database username = %q", username)
	}
	if password, ok := dsn.User.Password(); !ok || password != "p@ss:word" {
		t.Fatalf("database password was not encoded and restored correctly")
	}
}

func TestLoadAllowsDatabasePasswordEnvironmentOverride(t *testing.T) {
	resetEnvironment(t)
	t.Setenv("CONFIG_FILE", writeConfig(t, validConfigLines()...))
	t.Setenv("DATABASE_PASSWORD", "over@ride:/secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	dsn, err := url.Parse(cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	if password, ok := dsn.User.Password(); !ok || password != "over@ride:/secret" {
		t.Fatal("DATABASE_PASSWORD did not override the YAML password")
	}
}

func TestLoadAllowsFullDatabaseURLEnvironmentOverride(t *testing.T) {
	resetEnvironment(t)
	t.Setenv("CONFIG_FILE", writeConfig(t, "server:", "  address: 127.0.0.1:8080"))
	t.Setenv("DATABASE_URL", "postgres://app:secret@db.example:5432/ncode?sslmode=require")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DatabaseURL != "postgres://app:secret@db.example:5432/ncode?sslmode=require" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestLoadUsesDefaultYAMLConfig(t *testing.T) {
	resetEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Address != "127.0.0.1:8080" || cfg.AppEnv != "development" || !cfg.SeedDemo {
		t.Fatalf("default configuration = %+v", cfg)
	}
	if cfg.DatabaseURL != "postgres://ncode:ncode_local_dev@127.0.0.1:54329/ncode?sslmode=disable" {
		t.Fatalf("default database URL = %q", cfg.DatabaseURL)
	}
}

func TestLoadRejectsPublicBindAddress(t *testing.T) {
	resetEnvironment(t)
	t.Setenv("API_ADDR", "0.0.0.0:8080")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a non-loopback address")
	}
}

func TestLoadDisablesDemoSeedInProduction(t *testing.T) {
	resetEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("SEED_DEMO", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SeedDemo {
		t.Fatal("production configuration enabled demo data seeding")
	}
}

func TestLoadRejectsInvalidSeedFlag(t *testing.T) {
	resetEnvironment(t)
	t.Setenv("SEED_DEMO", "sometimes")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an invalid SEED_DEMO value")
	}
}

func TestLoadRejectsUnknownYAMLField(t *testing.T) {
	resetEnvironment(t)
	t.Setenv("CONFIG_FILE", writeConfig(t, append(validConfigLines(), "unknown: true")...))

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an unknown YAML field")
	}
}

func TestLoadRejectsInvalidDatabasePort(t *testing.T) {
	resetEnvironment(t)
	lines := validConfigLines()
	for i := range lines {
		if strings.HasPrefix(lines[i], "  url:") {
			lines[i] = "  url: jdbc:postgresql://db.example:70000/ncode_test"
		}
	}
	t.Setenv("CONFIG_FILE", writeConfig(t, lines...))

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an out-of-range database port")
	}
}

func resetEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"CONFIG_FILE",
		"API_ADDR",
		"DATABASE_URL",
		"DATABASE_PASSWORD",
		"APP_ENV",
		"SEED_DEMO",
	} {
		t.Setenv(name, "")
	}
}

func validConfigLines() []string {
	return []string{
		"server:",
		"  address: 127.0.0.1:8080",
		"  environment: development",
		"  seed_demo: true",
		"database:",
		"  url: jdbc:postgresql://db.example:5432/ncode_test?sslmode=require",
		"  username: ncode_app",
		"  password: 'p@ss:word'",
	}
}

func writeConfig(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	return path
}
