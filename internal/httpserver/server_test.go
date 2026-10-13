package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/cng1985/ncode/internal/config"
	"go.uber.org/fx"
)

func TestRegisterStartsAndGracefullyStopsServer(t *testing.T) {
	address := freeAddress(t)
	cfg := config.Config{Address: address, AppEnv: "test"}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	app := fx.New(
		fx.Supply(cfg, logger),
		fx.Provide(func() http.Handler { return handler }),
		fx.Invoke(Register),
		fx.NopLogger,
	)
	if err := app.Err(); err != nil {
		t.Fatalf("build Fx app: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("start Fx app: %v", err)
	}

	response, err := http.Get("http://" + address)
	if err != nil {
		t.Fatalf("request running server: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("response status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}

	if err := app.Stop(ctx); err != nil {
		t.Fatalf("stop Fx app: %v", err)
	}
	response, err = http.Get("http://" + address)
	if err == nil {
		_ = response.Body.Close()
		t.Fatal("HTTP server still accepted connections after Fx stopped")
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve ephemeral address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release ephemeral address: %v", err)
	}
	return address
}
