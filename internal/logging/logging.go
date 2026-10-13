package logging

import (
	"log/slog"
	"os"

	"github.com/cng1985/ncode/internal/config"
	"go.uber.org/fx"
)

var Module = fx.Module("logging", fx.Provide(New))

func New(cfg config.Config) *slog.Logger {
	if cfg.AppEnv == "development" {
		return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
