package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/cng1985/ncode/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type criterionInput struct {
	Text     string `json:"text"`
	Checked  bool   `json:"checked"`
	Position int    `json:"position"`
}

type workItemCreateRequest struct {
	ProjectID   int64            `json:"projectId"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Type        string           `json:"type"`
	Priority    string           `json:"priority"`
	OwnerName   string           `json:"ownerName"`
	DueDate     string           `json:"dueDate"`
	Criteria    []criterionInput `json:"criteria"`
}

type workItemUpdateRequest struct {
	ID              int64            `json:"id"`
	ExpectedVersion int              `json:"expectedVersion"`
	Title           string           `json:"title"`
	Description     string           `json:"description"`
	Type            string           `json:"type"`
	Priority        string           `json:"priority"`
	OwnerName       string           `json:"ownerName"`
	DueDate         string           `json:"dueDate"`
	Criteria        []criterionInput `json:"criteria"`
}

type workItemTransitionRequest struct {
	ID              int64  `json:"id"`
	ExpectedVersion int    `json:"expectedVersion"`
	Action          string `json:"action"`
	Reason          string `json:"reason"`
}

type acceptWorkItemRequest struct {
	ID                  int64  `json:"id"`
	ExpectedVersion     int    `json:"expectedVersion"`
	ChecklistVersion    int    `json:"checklistVersion"`
	AcceptanceStatement string `json:"acceptanceStatement"`
}

type workItemQuery struct {
	Page    int `json:"page"`
	Size    int `json:"size"`
	Filters struct {
		ProjectID int64  `json:"projectId"`
		State     *int   `json:"state"`
		Type      string `json:"type"`
		Priority  string `json:"priority"`
		Keyword   string `json:"keyword"`
	} `json:"filters"`
}

type criterionResponse struct {
	ID       int64  `json:"id"`
	Text     string `json:"text"`
	Checked  bool   `json:"checked"`
	Position int    `json:"position"`
}

type workItemResponse struct {
	ID               int64               `json:"id"`
	ProjectID        int64               `json:"projectId"`
	ProjectName      string              `json:"projectName"`
	Code             string              `json:"code"`
	Title            string              `json:"title"`
	Description      string              `json:"description"`
	Type             string              `json:"type"`
	Priority         string              `json:"priority"`
	State            int                 `json:"state"`
	StateName        string              `json:"stateName"`
	DisplayStateName string              `json:"displayStateName"`
	AcceptanceState  string              `json:"acceptanceState"`
	OwnerName        string              `json:"ownerName"`
	DueDate          string              `json:"dueDate"`
	Version          int                 `json:"version"`
	ChecklistVersion int                 `json:"checklistVersion"`
	Criteria         []criterionResponse `json:"criteria"`
	CreatedAt        string              `json:"createdAt"`
	UpdatedAt        string              `json:"updatedAt"`
}

func (h *handler) createWorkItem(c *gin.Context) {
	var req workItemCreateRequest
	body, ok := readBody(c, &req)
	if !ok {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.OwnerName = strings.TrimSpace(req.OwnerName)
	if req.Type == "" {
		req.Type = "TASK"
	}
	if req.Priority == "" {
		req.Priority = "MEDIUM"
	}
	if !validNewWorkItem(req.ProjectID, req.Title, req.Description, req.Type, req.Priority, req.DueDate, req.Criteria) {
		badRequest(c)
		return
	}
	if req.OwnerName == "" {
		req.OwnerName = "我"
	}
	h.writeAction(c, "workItem/create", body, func(tx *gorm.DB) (any, *apiError) {
		var project model.Project
		if err := tx.First(&project, req.ProjectID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, &apiError{status: http.StatusNotFound, code: 42002, msg: "project not found"}
			}
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		criteria := toCriteria(req.Criteria)
		item := model.WorkItem{ProjectID: project.ID, Title: req.Title, Description: req.Description, Type: req.Type, Priority: req.Priority, State: 0, AcceptanceState: "NONE", OwnerName: req.OwnerName, DueDate: model.LocalDate(req.DueDate), Version: 1, ChecklistVersion: 1, Criteria: criteria}
		if err := tx.Create(&item).Error; err != nil {
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		item.Code = model.CodeForWorkItem(item.ID)
		if err := tx.Model(&item).Update("code", item.Code).Error; err != nil || !model.SafeID(item.ID) {
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		if err := addActivity(tx, item, "我", "CREATED", "", item.Title); err != nil {
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		if err := tx.Preload("Criteria").Preload("Project").First(&item, item.ID).Error; err != nil {
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		return workItemView(item), nil
	})
}

func (h *handler) updateWorkItem(c *gin.Context) {
	var req workItemUpdateRequest
	if _, ok := readBody(c, &req); !ok {
		return
	}
	if !model.SafeID(req.ID) || req.ExpectedVersion < 1 || strings.TrimSpace(req.Title) == "" || len([]rune(req.Title)) > 180 || len(req.Description) > 20000 || !oneOf(req.Type, "TASK", "STORY", "BUG", "DEBT") || !oneOf(req.Priority, "LOW", "MEDIUM", "HIGH", "URGENT") || !validDate(req.DueDate) || !validCriteria(req.Criteria) {
		badRequest(c)
		return
	}
	tx := h.db.Begin()
	var item model.WorkItem
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Criteria").Preload("Project").First(&item, req.ID).Error; err != nil {
		tx.Rollback()
		if err == gorm.ErrRecordNotFound {
			failure(c, http.StatusNotFound, 42002, "work item not found", nil)
		} else {
			internalFailure(c)
		}
		return
	}
	if item.Version != req.ExpectedVersion {
		tx.Rollback()
		failure(c, http.StatusConflict, 47002, "work item has changed; reload before saving", gin.H{"currentVersion": item.Version})
		return
	}
	criteriaChanged := !sameCriteria(item.Criteria, req.Criteria)
	oldValue, _ := json.Marshal(gin.H{"title": item.Title, "priority": item.Priority, "ownerName": item.OwnerName, "dueDate": item.DueDate})
	item.Title = strings.TrimSpace(req.Title)
	item.Description = req.Description
	item.Type = req.Type
	item.Priority = req.Priority
	item.OwnerName = strings.TrimSpace(req.OwnerName)
	item.DueDate = model.LocalDate(req.DueDate)
	if item.OwnerName == "" {
		item.OwnerName = "我"
	}
	if criteriaChanged {
		if err := tx.Where("work_item_id = ?", item.ID).Delete(&model.AcceptanceCriterion{}).Error; err != nil {
			tx.Rollback()
			internalFailure(c)
			return
		}
		criteria := toCriteria(req.Criteria)
		if len(criteria) > 0 {
			for i := range criteria {
				criteria[i].WorkItemID = item.ID
			}
			if err := tx.Create(&criteria).Error; err != nil {
				tx.Rollback()
				internalFailure(c)
				return
			}
		}
		item.ChecklistVersion++
	}
	item.Version++
	if err := tx.Model(&item).Omit("Criteria").Updates(map[string]any{
		"title": item.Title, "description": item.Description, "type": item.Type, "priority": item.Priority,
		"owner_name": item.OwnerName, "due_date": item.DueDate, "version": item.Version,
		"checklist_version": item.ChecklistVersion, "updated_at": time.Now().UTC(),
	}).Error; err != nil {
		tx.Rollback()
		internalFailure(c)
		return
	}
	newValue, _ := json.Marshal(gin.H{"title": item.Title, "priority": item.Priority, "ownerName": item.OwnerName, "dueDate": item.DueDate, "checklistVersion": item.ChecklistVersion})
	if err := addActivity(tx, item, "我", "UPDATED", string(oldValue), string(newValue)); err != nil {
		tx.Rollback()
		internalFailure(c)
		return
	}
	if err := tx.Preload("Criteria").Preload("Project").First(&item, item.ID).Error; err != nil {
		tx.Rollback()
		internalFailure(c)
		return
	}
	if err := tx.Commit().Error; err != nil {
		internalFailure(c)
		return
	}
	success(c, workItemView(item))
}

func (h *handler) transitionWorkItem(c *gin.Context) {
	var req workItemTransitionRequest
	body, ok := readBody(c, &req)
	if !ok {
		return
	}
	if !model.SafeID(req.ID) || req.ExpectedVersion < 1 || !oneOf(req.Action, "READY", "START", "REQUEST_REVIEW", "MARK_ACCEPTANCE_PENDING", "REOPEN", "CANCEL") || len(req.Reason) > 500 {
		badRequest(c)
		return
	}
	h.writeAction(c, "workItem/transition", body, func(tx *gorm.DB) (any, *apiError) {
		var item model.WorkItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Criteria").Preload("Project").First(&item, req.ID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, &apiError{status: http.StatusNotFound, code: 42002, msg: "work item not found"}
			}
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		if item.Version != req.ExpectedVersion {
			return nil, &apiError{status: http.StatusConflict, code: 47002, msg: "work item has changed; reload before saving", data: gin.H{"currentVersion": item.Version}}
		}
		oldState, oldAcceptance := item.State, item.AcceptanceState
		switch req.Action {
		case "READY":
			if item.State != 0 || len(item.Criteria) == 0 || item.OwnerName == "" {
				return nil, stateError()
			}
			item.State = 1
		case "START":
			if item.State != 1 {
				return nil, stateError()
			}
			item.State = 2
		case "REQUEST_REVIEW":
			if item.State != 2 {
				return nil, stateError()
			}
			item.State = 3
		case "MARK_ACCEPTANCE_PENDING":
			if item.State != 3 {
				return nil, stateError()
			}
			item.AcceptanceState = "PENDING"
		case "REOPEN":
			if item.State != 4 || strings.TrimSpace(req.Reason) == "" {
				return nil, stateError()
			}
			item.State = 2
			item.AcceptanceState = "REWORK"
		case "CANCEL":
			if item.State == 4 || item.State == 5 {
				return nil, stateError()
			}
			item.State = 5
			item.AcceptanceState = "NONE"
		}
		item.Version++
		if err := tx.Model(&item).Updates(map[string]any{"state": item.State, "acceptance_state": item.AcceptanceState, "version": item.Version, "updated_at": time.Now().UTC()}).Error; err != nil {
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		oldValue := stateName(oldState) + "/" + oldAcceptance
		newValue := stateName(item.State) + "/" + item.AcceptanceState
		if strings.TrimSpace(req.Reason) != "" {
			newValue += ": " + strings.TrimSpace(req.Reason)
		}
		if err := addActivity(tx, item, "我", req.Action, oldValue, newValue); err != nil {
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		if err := tx.Preload("Criteria").Preload("Project").First(&item, item.ID).Error; err != nil {
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		return workItemView(item), nil
	})
}

func (h *handler) acceptWorkItem(c *gin.Context) {
	var req acceptWorkItemRequest
	body, ok := readBody(c, &req)
	if !ok {
		return
	}
	if !model.SafeID(req.ID) || req.ExpectedVersion < 1 || req.ChecklistVersion < 1 || len(req.AcceptanceStatement) > 1000 {
		badRequest(c)
		return
	}
	h.writeAction(c, "workItem/accept", body, func(tx *gorm.DB) (any, *apiError) {
		var item model.WorkItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Criteria").Preload("Project").First(&item, req.ID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, &apiError{status: http.StatusNotFound, code: 42002, msg: "work item not found"}
			}
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		if item.Version != req.ExpectedVersion || item.ChecklistVersion != req.ChecklistVersion {
			return nil, &apiError{status: http.StatusConflict, code: 47002, msg: "work item or acceptance checklist has changed", data: gin.H{"currentVersion": item.Version, "currentChecklistVersion": item.ChecklistVersion}}
		}
		if item.State != 3 || item.AcceptanceState != "PENDING" || len(item.Criteria) == 0 {
			return nil, stateError()
		}
		for _, criterion := range item.Criteria {
			if !criterion.Checked {
				return nil, &apiError{status: http.StatusConflict, code: 42001, msg: "all acceptance criteria must be checked before acceptance"}
			}
		}
		item.State = 4
		item.AcceptanceState = "ACCEPTED"
		item.Version++
		if err := tx.Model(&item).Updates(map[string]any{"state": item.State, "acceptance_state": item.AcceptanceState, "version": item.Version, "updated_at": time.Now().UTC()}).Error; err != nil {
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		if err := addActivity(tx, item, "我", "ACCEPTED", "待验收", "已验收："+strings.TrimSpace(req.AcceptanceStatement)); err != nil {
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		if err := tx.Preload("Criteria").Preload("Project").First(&item, item.ID).Error; err != nil {
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		return workItemView(item), nil
	})
}

func (h *handler) pageWorkItems(c *gin.Context) {
	var req workItemQuery
	if !bind(c, &req) {
		return
	}
	page, size, ok := paging(c, req.Page, req.Size)
	if !ok {
		return
	}
	if !model.SafeID(req.Filters.ProjectID) {
		badRequest(c)
		return
	}
	query := h.db.Model(&model.WorkItem{}).Where("project_id = ?", req.Filters.ProjectID)
	if req.Filters.State != nil {
		query = query.Where("state = ?", *req.Filters.State)
	}
	if req.Filters.Type != "" {
		query = query.Where("type = ?", req.Filters.Type)
	}
	if req.Filters.Priority != "" {
		query = query.Where("priority = ?", req.Filters.Priority)
	}
	if req.Filters.Keyword != "" {
		query = query.Where("title ILIKE ? OR description ILIKE ?", "%"+req.Filters.Keyword+"%", "%"+req.Filters.Keyword+"%")
	}
	var total int64
	if query.Count(&total).Error != nil {
		internalFailure(c)
		return
	}
	var items []model.WorkItem
	if query.Preload("Criteria").Preload("Project").Order("updated_at DESC").Order("id DESC").Offset((page-1)*size).Limit(size).Find(&items).Error != nil {
		internalFailure(c)
		return
	}
	data := make([]workItemResponse, 0, len(items))
	for _, item := range items {
		data = append(data, workItemView(item))
	}
	pageSuccess(c, data, page, size, total)
}

type activityQuery struct {
	WorkItemID int64 `json:"workItemId"`
}
type activityResponse struct {
	ID        int64  `json:"id"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	OldValue  string `json:"oldValue"`
	NewValue  string `json:"newValue"`
	Version   int    `json:"version"`
	CreatedAt string `json:"createdAt"`
}

func (h *handler) workItemActivity(c *gin.Context) {
	var req activityQuery
	if !bind(c, &req) || !model.SafeID(req.WorkItemID) {
		if !c.Writer.Written() {
			badRequest(c)
		}
		return
	}
	var rows []model.Activity
	if err := h.db.Where("work_item_id = ?", req.WorkItemID).Order("id DESC").Limit(100).Find(&rows).Error; err != nil {
		internalFailure(c)
		return
	}
	data := make([]activityResponse, 0, len(rows))
	for _, row := range rows {
		data = append(data, activityResponse{ID: row.ID, Actor: row.Actor, Action: row.Action, OldValue: row.OldValue, NewValue: row.NewValue, Version: row.Version, CreatedAt: row.CreatedAt.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05")})
	}
	success(c, data)
}

func workItemView(item model.WorkItem) workItemResponse {
	criteria := make([]criterionResponse, 0, len(item.Criteria))
	for _, criterion := range item.Criteria {
		criteria = append(criteria, criterionResponse{ID: criterion.ID, Text: criterion.Text, Checked: criterion.Checked, Position: criterion.Position})
	}
	display := stateName(item.State)
	if item.AcceptanceState == "PENDING" {
		display = "待验收"
	}
	zone := time.FixedZone("CST", 8*60*60)
	return workItemResponse{ID: item.ID, ProjectID: item.ProjectID, ProjectName: item.Project.Name, Code: item.Code, Title: item.Title, Description: item.Description, Type: item.Type, Priority: item.Priority, State: item.State, StateName: stateName(item.State), DisplayStateName: display, AcceptanceState: item.AcceptanceState, OwnerName: item.OwnerName, DueDate: dateOnly(string(item.DueDate)), Version: item.Version, ChecklistVersion: item.ChecklistVersion, Criteria: criteria, CreatedAt: item.CreatedAt.In(zone).Format("2006-01-02 15:04:05"), UpdatedAt: item.UpdatedAt.In(zone).Format("2006-01-02 15:04:05")}
}

func dateOnly(value string) string {
	if len(value) >= 10 {
		candidate := value[:10]
		if validDate(candidate) {
			return candidate
		}
	}
	return ""
}

func stateName(state int) string {
	return map[int]string{0: "待处理", 1: "就绪", 2: "进行中", 3: "待评审", 4: "已验收", 5: "已取消"}[state]
}

func validNewWorkItem(projectID int64, title, description, typ, priority, dueDate string, criteria []criterionInput) bool {
	return model.SafeID(projectID) && strings.TrimSpace(title) != "" && len([]rune(title)) <= 180 && len(description) <= 20000 && oneOf(typ, "TASK", "STORY", "BUG", "DEBT") && oneOf(priority, "LOW", "MEDIUM", "HIGH", "URGENT") && validDate(dueDate) && validCriteria(criteria)
}

func validDate(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, time.FixedZone("CST", 8*60*60))
	return err == nil && parsed.Format("2006-01-02") == value
}

func validCriteria(criteria []criterionInput) bool {
	if len(criteria) > 50 {
		return false
	}
	for _, item := range criteria {
		if strings.TrimSpace(item.Text) == "" || len([]rune(item.Text)) > 240 || item.Position < 0 || item.Position > 1000 {
			return false
		}
	}
	return true
}

func toCriteria(input []criterionInput) []model.AcceptanceCriterion {
	criteria := make([]model.AcceptanceCriterion, 0, len(input))
	for _, item := range input {
		criteria = append(criteria, model.AcceptanceCriterion{Text: strings.TrimSpace(item.Text), Checked: item.Checked, Position: item.Position})
	}
	return criteria
}

func sameCriteria(existing []model.AcceptanceCriterion, incoming []criterionInput) bool {
	if len(existing) != len(incoming) {
		return false
	}
	for i, criterion := range existing {
		if criterion.Text != strings.TrimSpace(incoming[i].Text) || criterion.Checked != incoming[i].Checked || criterion.Position != incoming[i].Position {
			return false
		}
	}
	return true
}

func addActivity(tx *gorm.DB, item model.WorkItem, actor, action, oldValue, newValue string) error {
	return tx.Create(&model.Activity{ProjectID: item.ProjectID, WorkItemID: item.ID, Actor: actor, Action: action, OldValue: oldValue, NewValue: newValue, Version: item.Version, CreatedAt: time.Now().UTC()}).Error
}

func stateError() *apiError {
	return &apiError{status: http.StatusConflict, code: 42001, msg: "work item cannot make this state transition"}
}
