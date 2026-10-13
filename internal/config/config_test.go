package config

import "testing"

func TestLoadDefaultsToLoopbackDevelopment(t *testing.T) {
	t.Setenv("API_ADDR", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("SEED_DEMO", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Address != "127.0.0.1:8080" {
		t.Fatalf("Address = %q, want loopback default", cfg.Address)
	}
	if cfg.AppEnv != "development" || !cfg.SeedDemo {
		t.Fatalf("development defaults = (%q, %v), want development with demo seed", cfg.AppEnv, cfg.SeedDemo)
	}
}

func TestLoadRejectsPublicBindAddress(t *testing.T) {
	t.Setenv("API_ADDR", "0.0.0.0:8080")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a non-loopback address")
	}
}

func TestLoadDisablesDemoSeedInProduction(t *testing.T) {
	t.Setenv("API_ADDR", "127.0.0.1:8080")
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
	t.Setenv("API_ADDR", "127.0.0.1:8080")
	t.Setenv("APP_ENV", "development")
	t.Setenv("SEED_DEMO", "sometimes")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an invalid SEED_DEMO value")
	}
}
