package identity

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cng1985/ncode/internal/config"
	"github.com/gin-gonic/gin"
)

func TestMiddlewareRejectsRequestsWithoutSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	service := New(nil, config.Config{})
	router.GET("/protected", service.Middleware(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/protected", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestProductionRegistrationRequiresConfiguredMailDelivery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	service := New(nil, config.Config{AppEnv: "production"})
	service.RegisterPublicRoutes(router.Group("/api/v1"))

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{"email":"person@example.com"}`)))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if strings.Contains(response.Body.String(), "developmentVerificationToken") {
		t.Fatal("production response exposed a development verification token")
	}
}
