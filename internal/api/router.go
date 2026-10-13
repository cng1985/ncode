package api

import (
	"net/http"

	"github.com/cng1985/ncode/internal/config"
	"github.com/cng1985/ncode/internal/supervisor"
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

var Module = fx.Module("api", fx.Provide(NewRouter))

type handler struct {
	db         *gorm.DB
	supervisor *supervisor.Manager
}

func NewRouter(db *gorm.DB, cfg config.Config, manager *supervisor.Manager) http.Handler {
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	h := &handler{db: db, supervisor: manager}
	r.GET("/healthz", func(c *gin.Context) {
		sqlDB, err := db.DB()
		if err != nil || sqlDB.PingContext(c.Request.Context()) != nil {
			failure(c, http.StatusServiceUnavailable, 50001, "service unavailable", nil)
			return
		}
		success(c, gin.H{"status": "ok"})
	})
	api := r.Group("/api/v1")
	api.POST("/project/create", h.createProject)
	api.POST("/project/query/page", h.pageProjects)
	api.POST("/project/query/one", h.oneProject)
	api.POST("/workItem/create", h.createWorkItem)
	api.POST("/workItem/update", h.updateWorkItem)
	api.POST("/workItem/transition", h.transitionWorkItem)
	api.POST("/workItem/accept", h.acceptWorkItem)
	api.POST("/workItem/query/page", h.pageWorkItems)
	api.POST("/workItem/query/activity", h.workItemActivity)
	api.POST("/service/create", h.createService)
	api.POST("/service/query/page", h.pageServices)
	api.POST("/service/query/one", h.oneService)
	api.POST("/service/start", h.startService)
	api.POST("/service/stop", h.stopService)
	api.POST("/service/query/logs", h.serviceLogs)
	return r
}
