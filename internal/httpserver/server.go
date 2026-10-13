package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/cng1985/ncode/internal/config"
	"go.uber.org/fx"
)

var Module = fx.Module("http-server", fx.Invoke(Register))

func Register(lifecycle fx.Lifecycle, cfg config.Config, handler http.Handler, logger *slog.Logger, shutdowner fx.Shutdowner) {
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	stopped := make(chan struct{})

	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			listener, err := net.Listen("tcp", cfg.Address)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", cfg.Address, err)
			}
			logger.Info("NCode API listening", "address", listener.Addr().String(), "environment", cfg.AppEnv)
			go func() {
				defer close(stopped)
				if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.Error("NCode API stopped unexpectedly", "error", err)
					if shutdownErr := shutdowner.Shutdown(fx.ExitCode(1)); shutdownErr != nil {
						logger.Error("request Fx shutdown after HTTP server failure", "error", shutdownErr)
					}
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if err := server.Shutdown(ctx); err != nil {
				_ = server.Close()
				return fmt.Errorf("shut down HTTP server: %w", err)
			}
			select {
			case <-stopped:
				return nil
			case <-ctx.Done():
				_ = server.Close()
				return ctx.Err()
			}
		},
	})
}
