package api

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
	"github.com/cng1985/ncode/internal/database"
	"github.com/cng1985/ncode/internal/identity"
	"github.com/cng1985/ncode/internal/model"
	"github.com/cng1985/ncode/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestProjectAndWorkItemAPIsAreWorkspaceScoped(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get integration database pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate integration database: %v", err)
	}

	marker := fmt.Sprintf("ncode-api-test-%d", time.Now().UnixNano())
	firstEmail, secondEmail := marker+"-first@example.test", marker+"-second@example.test"
	projectKey, otherProjectKey, workItemKey := marker+"-project", marker+"-other-project", marker+"-work-item"
	projectSlug := strings.ToLower(marker)
	t.Cleanup(func() {
		var users []model.User
		db.Where("email IN ?", []string{firstEmail, secondEmail}).Find(&users)
		userIDs := make([]int64, 0, len(users))
		for _, user := range users {
			userIDs = append(userIDs, user.ID)
		}
		if len(userIDs) > 0 {
			var projects []model.Project
			db.Where("owner_user_id IN ?", userIDs).Find(&projects)
			projectIDs := make([]int64, 0, len(projects))
			for _, project := range projects {
				projectIDs = append(projectIDs, project.ID)
			}
			var items []model.WorkItem
			if len(projectIDs) > 0 {
				db.Where("owner_user_id IN ?", userIDs).Delete(&model.Repository{})
				db.Where("project_id IN ?", projectIDs).Find(&items)
				itemIDs := make([]int64, 0, len(items))
				for _, item := range items {
					itemIDs = append(itemIDs, item.ID)
				}
				if len(itemIDs) > 0 {
					db.Where("work_item_id IN ?", itemIDs).Delete(&model.AcceptanceCriterion{})
					db.Where("work_item_id IN ?", itemIDs).Delete(&model.Activity{})
				}
				db.Where("project_id IN ?", projectIDs).Delete(&model.WorkItem{})
				db.Where("id IN ?", projectIDs).Delete(&model.Project{})
			}
			var memberships []model.Membership
			db.Where("user_id IN ?", userIDs).Find(&memberships)
			tenantIDs := make([]int64, 0, len(memberships))
			for _, membership := range memberships {
				tenantIDs = append(tenantIDs, membership.TenantID)
			}
			if len(tenantIDs) > 0 {
				db.Where("tenant_id IN ?", tenantIDs).Delete(&model.WorkspaceInvitation{})
				db.Where("tenant_id IN ?", tenantIDs).Delete(&model.Membership{})
				db.Where("id IN ?", tenantIDs).Delete(&model.Tenant{})
			}
			db.Where("user_id IN ?", userIDs).Delete(&model.Session{})
			db.Where("user_id IN ?", userIDs).Delete(&model.EmailVerification{})
			db.Where("id IN ?", userIDs).Delete(&model.User{})
		}
		db.Where("key IN ?", []string{projectKey, otherProjectKey, workItemKey}).Delete(&model.IdempotencyRecord{})
	})

	cfg := config.Config{AppEnv: "test", GitRoot: t.TempDir()}
	router := NewRouter(db, cfg, identity.New(db, cfg), repository.New(db, cfg))
	firstCookie := registerTestAccount(t, router, firstEmail)
	secondCookie := registerTestAccount(t, router, secondEmail)
	projectResponse := postTestAPI(t, router, "/api/v1/project/create", fmt.Sprintf(`{"name":"Isolation test","slug":%q,"goal":"test tenant scoping"}`, projectSlug), firstCookie, "", projectKey)
	var project struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(projectResponse.Data, &project); err != nil || project.ID <= 0 {
		t.Fatalf("project response was invalid: %s", projectResponse.Data)
	}
	if response := postTestRaw(router, "/api/v1/project/create", fmt.Sprintf(`{"name":"Public project","slug":%q,"visibility":"public"}`, projectSlug+"-public"), firstCookie, "", marker+"-public-project"); response.Code != http.StatusBadRequest {
		t.Fatalf("unsupported public project visibility returned %d, want 400", response.Code)
	}
	if response := postTestRaw(router, "/api/v1/repository/create", fmt.Sprintf(`{"projectId":%d,"name":"Public code","slug":%q,"visibility":"public"}`, project.ID, projectSlug+"-public-code"), firstCookie, "", ""); response.Code != http.StatusBadRequest {
		t.Fatalf("unsupported public repository visibility returned %d, want 400", response.Code)
	}
	repositoryResponse := postTestAPI(t, router, "/api/v1/repository/create", fmt.Sprintf(`{"projectId":%d,"name":"Isolation code","slug":%q,"visibility":"private"}`, project.ID, projectSlug+"-code"), firstCookie, "", "")
	var repositoryData struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(repositoryResponse.Data, &repositoryData); err != nil || repositoryData.ID <= 0 {
		t.Fatalf("repository response was invalid: %s", repositoryResponse.Data)
	}
	treeResponse := postTestAPI(t, router, "/api/v1/repository/query/tree", fmt.Sprintf(`{"repositoryId":%d,"ref":"main"}`, repositoryData.ID), firstCookie, "", "")
	if !strings.Contains(string(treeResponse.Data), "README.md") {
		t.Fatal("new repository tree did not include its README")
	}
	readmeResponse := postTestAPI(t, router, "/api/v1/repository/query/file", fmt.Sprintf(`{"repositoryId":%d,"ref":"main","path":"README.md"}`, repositoryData.ID), firstCookie, "", "")
	if !strings.Contains(string(readmeResponse.Data), "Repository for project delivery") {
		t.Fatal("new repository did not return its initial README contents")
	}

	otherProjects := postTestAPI(t, router, "/api/v1/project/query/page", `{"page":1,"size":20,"filters":{}}`, secondCookie, "", "")
	if otherProjects.Total != 0 {
		t.Fatalf("second workspace saw %d projects, want 0", otherProjects.Total)
	}
	if response := postTestRaw(router, "/api/v1/project/query/one", fmt.Sprintf(`{"id":%d}`, project.ID), secondCookie, "", ""); response.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace project lookup returned %d, want 404", response.Code)
	}
	otherProject := postTestAPI(t, router, "/api/v1/project/create", fmt.Sprintf(`{"name":"Other workspace project","slug":%q}`, projectSlug), secondCookie, "", otherProjectKey)
	if otherProject.Code != 200 {
		t.Fatal("another workspace could not use the same project slug")
	}
	otherProjects = postTestAPI(t, router, "/api/v1/project/query/page", `{"page":1,"size":20,"filters":{}}`, secondCookie, "", "")
	if otherProjects.Total != 1 {
		t.Fatalf("second workspace project list returned %d projects, want 1", otherProjects.Total)
	}
	if response := postTestRaw(router, "/api/v1/repository/query/tree", fmt.Sprintf(`{"repositoryId":%d,"ref":"main"}`, repositoryData.ID), secondCookie, "", ""); response.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace repository lookup returned %d, want 404", response.Code)
	}
	if response := postTestRaw(router, "/api/v1/repository/query/tree", fmt.Sprintf(`{"repositoryId":%d,"ref":"main","path":"../"}`, repositoryData.ID), firstCookie, "", ""); response.Code != http.StatusBadRequest {
		t.Fatalf("repository traversal path returned %d, want 400", response.Code)
	}
	if response := postTestRaw(router, "/api/v1/workItem/create", fmt.Sprintf(`{"projectId":%d,"title":"forbidden","type":"TASK","priority":"MEDIUM","criteria":[]}`, project.ID), secondCookie, "", marker+"-forbidden"); response.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace work item creation returned %d, want 404", response.Code)
	}

	createdItem := postTestAPI(t, router, "/api/v1/workItem/create", fmt.Sprintf(`{"projectId":%d,"title":"Tenant scoped item","type":"TASK","priority":"MEDIUM","criteria":[{"text":"test access boundary","position":0}]}`, project.ID), firstCookie, "", workItemKey)
	var item struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(createdItem.Data, &item); err != nil || item.ID <= 0 {
		t.Fatalf("work item response was invalid: %s", createdItem.Data)
	}
	if response := postTestRaw(router, "/api/v1/workItem/query/activity", fmt.Sprintf(`{"workItemId":%d}`, item.ID), secondCookie, "", ""); response.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace activity lookup returned %d, want 404", response.Code)
	}
}

type testAPIResponse struct {
	Code  int             `json:"code"`
	Msg   string          `json:"msg"`
	Data  json.RawMessage `json:"data"`
	Total int64           `json:"total"`
}

func registerTestAccount(t *testing.T, router http.Handler, email string) *http.Cookie {
	t.Helper()
	registered := postTestAPI(t, router, "/api/v1/auth/register", fmt.Sprintf(`{"email":%q,"name":"Tenant Test","password":"correct horse 42"}`, email), nil, "", "")
	var registration struct {
		Token string `json:"developmentVerificationToken"`
	}
	if err := json.Unmarshal(registered.Data, &registration); err != nil || registration.Token == "" {
		t.Fatal("development email verification token was not returned")
	}
	response := postTestRaw(router, "/api/v1/auth/verify", fmt.Sprintf(`{"token":%q}`, registration.Token), nil, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("email verification returned %d", response.Code)
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == identity.SessionCookie {
			return cookie
		}
	}
	t.Fatal("email verification did not issue a session cookie")
	return nil
}

func postTestAPI(t *testing.T, router http.Handler, path, body string, cookie *http.Cookie, workspace, idempotencyKey string) testAPIResponse {
	t.Helper()
	response := postTestRaw(router, path, body, cookie, workspace, idempotencyKey)
	if response.Code != http.StatusOK {
		t.Fatalf("POST %s returned %d", path, response.Code)
	}
	var result testAPIResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Code != 200 {
		t.Fatalf("POST %s returned an invalid response", path)
	}
	return result
}

func postTestRaw(router http.Handler, path, body string, cookie *http.Cookie, workspace, idempotencyKey string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if workspace != "" {
		request.Header.Set("X-Workspace-ID", workspace)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
