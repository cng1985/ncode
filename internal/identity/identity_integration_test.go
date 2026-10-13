package identity

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cng1985/ncode/internal/config"
	"github.com/cng1985/ncode/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestAccountWorkspaceAndInvitationFlow(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get integration database pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.Tenant{}, &model.Membership{}, &model.EmailVerification{}, &model.Session{}, &model.WorkspaceInvitation{}); err != nil {
		t.Fatalf("migrate integration database: %v", err)
	}

	marker := fmt.Sprintf("ncode-test-%d", time.Now().UnixNano())
	firstEmail, secondEmail := marker+"-first@example.test", marker+"-second@example.test"
	t.Cleanup(func() {
		var users []model.User
		db.Where("email IN ?", []string{firstEmail, secondEmail}).Find(&users)
		userIDs := make([]int64, 0, len(users))
		for _, user := range users {
			userIDs = append(userIDs, user.ID)
		}
		if len(userIDs) > 0 {
			var memberships []model.Membership
			db.Where("user_id IN ?", userIDs).Find(&memberships)
			tenantIDs := make([]int64, 0, len(memberships))
			for _, membership := range memberships {
				tenantIDs = append(tenantIDs, membership.TenantID)
			}
			db.Where("user_id IN ?", userIDs).Delete(&model.Session{})
			db.Where("user_id IN ?", userIDs).Delete(&model.EmailVerification{})
			db.Where("user_id IN ?", userIDs).Delete(&model.Membership{})
			if len(tenantIDs) > 0 {
				db.Where("tenant_id IN ?", tenantIDs).Delete(&model.WorkspaceInvitation{})
				db.Where("id IN ?", tenantIDs).Delete(&model.Tenant{})
			}
			db.Where("id IN ?", userIDs).Delete(&model.User{})
		}
	})

	service := New(db, config.Config{AppEnv: "development"})
	router := gin.New()
	root := router.Group("/api/v1")
	service.RegisterPublicRoutes(root)
	protected := router.Group("/api/v1")
	protected.Use(service.Middleware())
	service.RegisterRoutes(protected)

	firstCookie := registerAndVerify(t, router, firstEmail)
	createdWorkspace := requestAPI(t, router, http.MethodPost, "/api/v1/workspace/create", `{"name":"Team Alpha"}`, firstCookie, "")
	var firstWorkspace struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(createdWorkspace.Data, &firstWorkspace); err != nil || firstWorkspace.ID <= 0 {
		t.Fatalf("created workspace response was invalid: %s", createdWorkspace.Data)
	}
	invite := requestAPI(t, router, http.MethodPost, "/api/v1/workspace/invite/create", fmt.Sprintf(`{"email":%q}`, secondEmail), firstCookie, fmt.Sprint(firstWorkspace.ID))
	var inviteData struct {
		Token string `json:"developmentInvitationToken"`
	}
	if err := json.Unmarshal(invite.Data, &inviteData); err != nil || inviteData.Token == "" {
		t.Fatalf("invite response did not include a development token: %s", invite.Data)
	}

	secondCookie := registerAndVerify(t, router, secondEmail)
	requestAPI(t, router, http.MethodPost, "/api/v1/workspace/invite/accept", fmt.Sprintf(`{"token":%q}`, inviteData.Token), secondCookie, "")
	requestAPI(t, router, http.MethodGet, "/api/v1/auth/me", "", secondCookie, fmt.Sprint(firstWorkspace.ID))
	if response := requestRaw(router, http.MethodGet, "/api/v1/auth/me", nil, firstCookie, fmt.Sprint(firstWorkspace.ID+100000)); response.Code != http.StatusForbidden {
		t.Fatalf("other workspace access returned status %d, want 403", response.Code)
	}
	thirdEmail := marker + "-third@example.test"
	pendingInvite := requestAPI(t, router, http.MethodPost, "/api/v1/workspace/invite/create", fmt.Sprintf(`{"email":%q}`, thirdEmail), firstCookie, fmt.Sprint(firstWorkspace.ID))
	var pendingInviteData struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(pendingInvite.Data, &pendingInviteData); err != nil || pendingInviteData.ID <= 0 {
		t.Fatalf("pending invitation response was invalid: %s", pendingInvite.Data)
	}
	requestAPI(t, router, http.MethodPost, "/api/v1/workspace/invite/revoke", fmt.Sprintf(`{"id":%d}`, pendingInviteData.ID), firstCookie, fmt.Sprint(firstWorkspace.ID))
	if response := requestRaw(router, http.MethodGet, "/api/v1/auth/me", nil, secondCookie, ""); response.Code != http.StatusOK {
		t.Fatalf("second account session returned status %d, want 200", response.Code)
	}
	requestAPI(t, router, http.MethodPost, "/api/v1/auth/logout", `{}`, firstCookie, "")
	if response := requestRaw(router, http.MethodGet, "/api/v1/auth/me", nil, firstCookie, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session returned status %d, want 401", response.Code)
	}
}

func registerAndVerify(t *testing.T, router http.Handler, email string) *http.Cookie {
	t.Helper()
	registered := requestAPI(t, router, http.MethodPost, "/api/v1/auth/register", fmt.Sprintf(`{"email":%q,"name":"Test User","password":"correct horse 42"}`, email), nil, "")
	var registration struct {
		Token string `json:"developmentVerificationToken"`
	}
	if err := json.Unmarshal(registered.Data, &registration); err != nil || registration.Token == "" {
		t.Fatalf("registration response did not include a development verification token: %s", registered.Data)
	}
	response := requestRaw(router, http.MethodPost, "/api/v1/auth/verify", strings.NewReader(fmt.Sprintf(`{"token":%q}`, registration.Token)), nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("verify email returned status %d: %s", response.Code, response.Body.String())
	}
	if replay := requestRaw(router, http.MethodPost, "/api/v1/auth/verify", strings.NewReader(fmt.Sprintf(`{"token":%q}`, registration.Token)), nil, ""); replay.Code != http.StatusBadRequest {
		t.Fatalf("replayed verification token returned status %d, want 400", replay.Code)
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == SessionCookie && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatal("email verification did not issue a session cookie")
	return nil
}

type apiResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func requestAPI(t *testing.T, router http.Handler, method, path, body string, cookie *http.Cookie, workspace string) apiResponse {
	t.Helper()
	response := requestRaw(router, method, path, strings.NewReader(body), cookie, workspace)
	if response.Code != http.StatusOK {
		t.Fatalf("%s %s returned status %d: %s", method, path, response.Code, response.Body.String())
	}
	var result apiResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Code != 200 {
		t.Fatalf("%s %s returned invalid API response: %s", method, path, response.Body.String())
	}
	return result
}

func requestRaw(router http.Handler, method, path string, body *strings.Reader, cookie *http.Cookie, workspace string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != nil {
		reader = body
	} else {
		reader = strings.NewReader("")
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if workspace != "" {
		request.Header.Set("X-Workspace-ID", workspace)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
