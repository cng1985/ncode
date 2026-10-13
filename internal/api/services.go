package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cng1985/ncode/internal/model"
	"github.com/cng1985/ncode/internal/supervisor"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type createServiceRequest struct {
	Name        string   `json:"name"`
	Code        string   `json:"code"`
	Description string   `json:"description"`
	Kind        string   `json:"kind"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	WorkDir     string   `json:"workDir"`
}

type serviceResponse struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Code        string   `json:"code"`
	Description string   `json:"description"`
	Kind        string   `json:"kind"`
	KindName    string   `json:"kindName"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	WorkDir     string   `json:"workDir"`
	State       int      `json:"state"`
	StateName   string   `json:"stateName"`
	PID         int      `json:"pid"`
	ExitCode    *int     `json:"exitCode"`
	Version     int      `json:"version"`
	StartedAt   string   `json:"startedAt"`
	StoppedAt   string   `json:"stoppedAt"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

type serviceLogResponse struct {
	ID        int64  `json:"id"`
	ServiceID int64  `json:"serviceId"`
	Stream    string `json:"stream"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}

func (h *handler) createService(c *gin.Context) {
	var req createServiceRequest
	body, ok := readBody(c, &req)
	if !ok {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Code = strings.TrimSpace(strings.ToLower(req.Code))
	req.Description = strings.TrimSpace(req.Description)
	req.Kind = strings.TrimSpace(req.Kind)
	req.Command = strings.TrimSpace(req.Command)
	req.WorkDir = strings.TrimSpace(req.WorkDir)
	if req.Args == nil {
		req.Args = []string{}
	}
	if req.Name == "" || len([]rune(req.Name)) > 120 || !slugPattern.MatchString(req.Code) || len(req.Description) > 500 {
		badRequest(c)
		return
	}
	if _, err := supervisor.Prepare(supervisor.Spec{Kind: req.Kind, Command: req.Command, Args: req.Args, WorkDir: req.WorkDir}); err != nil {
		h.writeServiceError(c, err)
		return
	}
	encoded, err := json.Marshal(req.Args)
	if err != nil {
		badRequest(c)
		return
	}
	h.writeAction(c, "service/create", body, func(tx *gorm.DB) (any, *apiError) {
		service := model.SystemService{
			Name:        req.Name,
			Code:        req.Code,
			Description: req.Description,
			Kind:        req.Kind,
			Command:     req.Command,
			Args:        string(encoded),
			WorkDir:     req.WorkDir,
			State:       model.ServiceStopped,
			Version:     1,
		}
		if err := tx.Create(&service).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
				return nil, &apiError{status: http.StatusConflict, code: 43002, msg: "service code is already in use"}
			}
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		if !model.SafeID(service.ID) {
			return nil, &apiError{status: http.StatusInternalServerError, code: 50001, msg: "request could not be completed"}
		}
		return serviceView(service), nil
	})
}

type serviceQuery struct {
	Page    int `json:"page"`
	Size    int `json:"size"`
	Filters struct {
		Keyword string `json:"keyword"`
		Kind    string `json:"kind"`
		State   *int   `json:"state"`
	} `json:"filters"`
}

func (h *handler) pageServices(c *gin.Context) {
	var req serviceQuery
	if !bind(c, &req) {
		return
	}
	page, size, ok := paging(c, req.Page, req.Size)
	if !ok {
		return
	}
	query := h.db.Model(&model.SystemService{})
	keyword := strings.TrimSpace(req.Filters.Keyword)
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name ILIKE ? OR code ILIKE ? OR description ILIKE ? OR command ILIKE ?", like, like, like, like)
	}
	if req.Filters.Kind != "" {
		query = query.Where("kind = ?", req.Filters.Kind)
	}
	if req.Filters.State != nil {
		query = query.Where("state = ?", *req.Filters.State)
	}
	var total int64
	if query.Count(&total).Error != nil {
		internalFailure(c)
		return
	}
	var services []model.SystemService
	if query.Order("updated_at DESC").Order("id DESC").Offset((page-1)*size).Limit(size).Find(&services).Error != nil {
		internalFailure(c)
		return
	}
	data := make([]serviceResponse, 0, len(services))
	for _, service := range services {
		data = append(data, serviceView(service))
	}
	pageSuccess(c, data, page, size, total)
}

type oneServiceRequest struct {
	ID int64 `json:"id"`
}

func (h *handler) oneService(c *gin.Context) {
	var req oneServiceRequest
	if !bind(c, &req) || !model.SafeID(req.ID) {
		if !c.Writer.Written() {
			badRequest(c)
		}
		return
	}
	service, err := h.findService(req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failure(c, http.StatusNotFound, 43002, "service not found", nil)
			return
		}
		internalFailure(c)
		return
	}
	success(c, serviceView(service))
}

type serviceActionRequest struct {
	ID              int64 `json:"id"`
	ExpectedVersion int   `json:"expectedVersion"`
}

func (h *handler) startService(c *gin.Context) {
	service, ok := h.lockServiceAction(c, false)
	if !ok {
		return
	}
	spec, err := specFromModel(service)
	if err != nil {
		badRequest(c)
		return
	}
	if err := h.supervisor.Start(spec); err != nil {
		h.writeServiceError(c, err)
		return
	}
	h.respondService(c, service.ID)
}

func (h *handler) stopService(c *gin.Context) {
	service, ok := h.lockServiceAction(c, true)
	if !ok {
		return
	}
	if err := h.supervisor.Stop(service.ID); err != nil {
		h.writeServiceError(c, err)
		return
	}
	h.respondService(c, service.ID)
}

func (h *handler) lockServiceAction(c *gin.Context, mustRun bool) (model.SystemService, bool) {
	var req serviceActionRequest
	if !bind(c, &req) || !model.SafeID(req.ID) || req.ExpectedVersion < 1 {
		if !c.Writer.Written() {
			badRequest(c)
		}
		return model.SystemService{}, false
	}
	service, err := h.findService(req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failure(c, http.StatusNotFound, 43002, "service not found", nil)
			return model.SystemService{}, false
		}
		internalFailure(c)
		return model.SystemService{}, false
	}
	if service.Version != req.ExpectedVersion {
		failure(c, http.StatusConflict, 47002, "service has changed; reload before retrying", gin.H{"currentVersion": service.Version})
		return model.SystemService{}, false
	}
	if mustRun && service.State != model.ServiceRunning {
		failure(c, http.StatusConflict, 47002, "service is not running", nil)
		return model.SystemService{}, false
	}
	if !mustRun && service.State == model.ServiceRunning {
		failure(c, http.StatusConflict, 47002, "service is already running", nil)
		return model.SystemService{}, false
	}
	return service, true
}

type serviceLogQuery struct {
	ServiceID int64 `json:"serviceId"`
	AfterID   int64 `json:"afterId"`
	Size      int   `json:"size"`
}

func (h *handler) serviceLogs(c *gin.Context) {
	var req serviceLogQuery
	if !bind(c, &req) || !model.SafeID(req.ServiceID) || req.AfterID < 0 {
		if !c.Writer.Written() {
			badRequest(c)
		}
		return
	}
	size := req.Size
	if size == 0 {
		size = 200
	}
	if size < 1 || size > 500 {
		failure(c, http.StatusBadRequest, 41001, "size must be between 1 and 500", nil)
		return
	}
	if _, err := h.findService(req.ServiceID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failure(c, http.StatusNotFound, 43002, "service not found", nil)
			return
		}
		internalFailure(c)
		return
	}
	var logs []model.ServiceLog
	query := h.db.Where("service_id = ?", req.ServiceID)
	if req.AfterID > 0 {
		if query.Where("id > ?", req.AfterID).Order("id ASC").Limit(size).Find(&logs).Error != nil {
			internalFailure(c)
			return
		}
	} else if query.Order("id DESC").Limit(size).Find(&logs).Error != nil {
		internalFailure(c)
		return
	} else {
		for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
			logs[i], logs[j] = logs[j], logs[i]
		}
	}
	data := make([]serviceLogResponse, 0, len(logs))
	for _, entry := range logs {
		data = append(data, serviceLogResponse{
			ID:        entry.ID,
			ServiceID: entry.ServiceID,
			Stream:    entry.Stream,
			Message:   entry.Message,
			CreatedAt: formatTimestamp(entry.CreatedAt, true),
		})
	}
	success(c, data)
}

func (h *handler) respondService(c *gin.Context, id int64) {
	service, err := h.findService(id)
	if err != nil {
		internalFailure(c)
		return
	}
	success(c, serviceView(service))
}

func (h *handler) findService(id int64) (model.SystemService, error) {
	var service model.SystemService
	err := h.db.First(&service, id).Error
	return service, err
}

func (h *handler) writeServiceError(c *gin.Context, err error) {
	var commandErr *supervisor.CommandError
	if errors.As(err, &commandErr) {
		failure(c, http.StatusBadRequest, 41001, commandErr.Reason, nil)
		return
	}
	if errors.Is(err, supervisor.ErrAlreadyRunning) {
		failure(c, http.StatusConflict, 47002, "service is already running", nil)
		return
	}
	if errors.Is(err, supervisor.ErrNotRunning) {
		failure(c, http.StatusConflict, 47002, "service is not running", nil)
		return
	}
	if errors.Is(err, supervisor.ErrTooMany) {
		failure(c, http.StatusConflict, 47002, "too many services are running", nil)
		return
	}
	internalFailure(c)
}

func specFromModel(service model.SystemService) (supervisor.Spec, error) {
	var args []string
	if strings.TrimSpace(service.Args) != "" {
		if err := json.Unmarshal([]byte(service.Args), &args); err != nil {
			return supervisor.Spec{}, err
		}
	}
	return supervisor.Spec{ID: service.ID, Kind: service.Kind, Command: service.Command, Args: args, WorkDir: service.WorkDir}, nil
}

func serviceView(service model.SystemService) serviceResponse {
	args := []string{}
	if strings.TrimSpace(service.Args) != "" {
		_ = json.Unmarshal([]byte(service.Args), &args)
	}
	if args == nil {
		args = []string{}
	}
	kindName := "进程"
	if service.Kind == model.ServiceKindBuiltin {
		kindName = "内置"
	}
	stateName := "已停止"
	switch service.State {
	case model.ServiceRunning:
		stateName = "运行中"
	case model.ServiceFailed:
		stateName = "已失败"
	}
	return serviceResponse{
		ID:          service.ID,
		Name:        service.Name,
		Code:        service.Code,
		Description: service.Description,
		Kind:        service.Kind,
		KindName:    kindName,
		Command:     service.Command,
		Args:        args,
		WorkDir:     service.WorkDir,
		State:       service.State,
		StateName:   stateName,
		PID:         service.PID,
		ExitCode:    service.ExitCode,
		Version:     service.Version,
		StartedAt:   formatTimestampPtr(service.StartedAt, false),
		StoppedAt:   formatTimestampPtr(service.StoppedAt, false),
		CreatedAt:   formatTimestamp(service.CreatedAt, false),
		UpdatedAt:   formatTimestamp(service.UpdatedAt, false),
	}
}

func formatTimestamp(value time.Time, withMillis bool) string {
	if value.IsZero() {
		return ""
	}
	layout := "2006-01-02 15:04:05"
	if withMillis {
		layout = "2006-01-02 15:04:05.000"
	}
	return value.In(time.FixedZone("CST", 8*60*60)).Format(layout)
}

func formatTimestampPtr(value *time.Time, withMillis bool) string {
	if value == nil {
		return ""
	}
	return formatTimestamp(*value, withMillis)
}
