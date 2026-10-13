package api

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cng1985/ncode/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type createProjectRequest struct {
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Goal       string `json:"goal"`
	Type       string `json:"type"`
	Visibility string `json:"visibility"`
}

type projectResponse struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Goal       string `json:"goal"`
	Type       string `json:"type"`
	Visibility string `json:"visibility"`
	State      int    `json:"state"`
	StateName  string `json:"stateName"`
	OwnerName  string `json:"ownerName"`
	Progress   int    `json:"progress"`
	ItemCount  int64  `json:"itemCount"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func (h *handler) createProject(c *gin.Context) {
	var req createProjectRequest
	body, ok := readBody(c, &req)
	if !ok {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.TrimSpace(strings.ToLower(req.Slug))
	req.Goal = strings.TrimSpace(req.Goal)
	if req.Type == "" {
		req.Type = "product"
	}
	if req.Visibility == "" {
		req.Visibility = "private"
	}
	if req.Name == "" || len([]rune(req.Name)) > 120 || !slugPattern.MatchString(req.Slug) || len(req.Goal) > 500 || !oneOf(req.Type, "learning", "open_source", "product", "team") || !oneOf(req.Visibility, "private", "internal", "public") {
		badRequest(c)
		return
	}
	h.writeAction(c, "project/create", body, func(tx *gorm.DB) (any, *apiError) {
		project := model.Project{Name: req.Name, Slug: req.Slug, Goal: req.Goal, Type: req.Type, Visibility: req.Visibility, State: 1, OwnerName: "我"}
		if err := tx.Create(&project).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
				return nil, &apiError{status: http.StatusConflict, code: 42002, msg: "project slug is already in use"}
			}
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		if !model.SafeID(project.ID) {
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		return projectView(tx, project), nil
	})
}

type projectQuery struct {
	Page    int `json:"page"`
	Size    int `json:"size"`
	Filters struct {
		Keyword string `json:"keyword"`
		Type    string `json:"type"`
		State   *int   `json:"state"`
	} `json:"filters"`
}

func (h *handler) pageProjects(c *gin.Context) {
	var req projectQuery
	if !bind(c, &req) {
		return
	}
	page, size, ok := paging(c, req.Page, req.Size)
	if !ok {
		return
	}
	query := h.db.Model(&model.Project{})
	if req.Filters.Keyword != "" {
		query = query.Where("name ILIKE ? OR goal ILIKE ?", "%"+req.Filters.Keyword+"%", "%"+req.Filters.Keyword+"%")
	}
	if req.Filters.Type != "" {
		query = query.Where("type = ?", req.Filters.Type)
	}
	if req.Filters.State != nil {
		query = query.Where("state = ?", *req.Filters.State)
	}
	var total int64
	if query.Count(&total).Error != nil {
		internalFailure(c)
		return
	}
	var projects []model.Project
	if query.Order("updated_at DESC").Order("id DESC").Offset((page-1)*size).Limit(size).Find(&projects).Error != nil {
		internalFailure(c)
		return
	}
	data := make([]projectResponse, 0, len(projects))
	for _, project := range projects {
		data = append(data, projectView(h.db, project))
	}
	pageSuccess(c, data, page, size, total)
}

type oneProjectRequest struct {
	ID int64 `json:"id"`
}

func (h *handler) oneProject(c *gin.Context) {
	var req oneProjectRequest
	if !bind(c, &req) || !model.SafeID(req.ID) {
		if !c.Writer.Written() {
			badRequest(c)
		}
		return
	}
	var project model.Project
	if err := h.db.First(&project, req.ID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			failure(c, http.StatusNotFound, 42002, "project not found", nil)
			return
		}
		internalFailure(c)
		return
	}
	success(c, projectView(h.db, project))
}

func projectView(db *gorm.DB, project model.Project) projectResponse {
	var total int64
	db.Model(&model.WorkItem{}).Where("project_id = ?", project.ID).Count(&total)
	progress := 0
	if total > 0 {
		db.Model(&model.WorkItem{}).Where("project_id = ?", project.ID).
			Select("COALESCE(ROUND(AVG(CASE WHEN state = 4 THEN 100 WHEN state = 3 AND acceptance_state = 'PENDING' THEN 90 WHEN state = 3 THEN 75 WHEN state = 2 THEN 50 WHEN state = 1 THEN 20 ELSE 0 END)), 0)").
			Scan(&progress)
	}
	stateName := map[int]string{0: "草稿", 1: "进行中", 2: "已暂停", 3: "已完成", 4: "已归档"}[project.State]
	return projectResponse{ID: project.ID, Name: project.Name, Slug: project.Slug, Goal: project.Goal, Type: project.Type, Visibility: project.Visibility, State: project.State, StateName: stateName, OwnerName: project.OwnerName, Progress: progress, ItemCount: total, CreatedAt: project.CreatedAt.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05"), UpdatedAt: project.UpdatedAt.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05")}
}
