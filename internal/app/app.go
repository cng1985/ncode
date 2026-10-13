package app

import (
	"github.com/cng1985/ncode/internal/api"
	"github.com/cng1985/ncode/internal/config"
	"github.com/cng1985/ncode/internal/database"
	"github.com/cng1985/ncode/internal/httpserver"
	"github.com/cng1985/ncode/internal/logging"
	"go.uber.org/fx"
)

var Module = fx.Module(
	"ncode",
	config.Module,
	logging.Module,
	database.Module,
	api.Module,
	httpserver.Module,
)

func New() *fx.App {
	return fx.New(Module)
}
