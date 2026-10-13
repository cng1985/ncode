package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type handler struct{ db *gorm.DB }

func NewRouter(db *gorm.DB) http.Handler {
	r := gin.New()
	r.Use(gin.Recovery())
	h := &handler{db: db}
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
	return r
}
