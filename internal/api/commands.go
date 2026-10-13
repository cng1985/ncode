package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cng1985/ncode/internal/identity"
	"github.com/cng1985/ncode/internal/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func readBody(c *gin.Context, target any) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil || len(body) == 0 || json.Unmarshal(body, target) != nil {
		badRequest(c)
		return nil, false
	}
	return body, true
}

func (h *handler) writeAction(c *gin.Context, action string, body []byte, operation func(*gorm.DB) (any, *apiError)) {
	// Idempotency keys are scoped to the active workspace so a replay cannot
	// return another workspace's previously stored response.
	action = fmt.Sprintf("workspace:%d:%s", identity.WorkspaceID(c), action)
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" || len(key) > 120 {
		failure(c, http.StatusBadRequest, 41001, "Idempotency-Key is required", nil)
		return
	}
	sum := sha256.Sum256(body)
	requestHash := hex.EncodeToString(sum[:])
	tx := h.db.Begin()
	if tx.Error != nil {
		failure(c, http.StatusInternalServerError, 50001, "request could not be completed", nil)
		return
	}
	record := model.IdempotencyRecord{Key: key, Action: action, RequestHash: requestHash, Status: http.StatusOK, Response: []byte(`{}`)}
	if err := tx.Create(&record).Error; err != nil {
		tx.Rollback()
		var prior model.IdempotencyRecord
		if lookupErr := h.db.Where("key = ? AND action = ?", key, action).First(&prior).Error; lookupErr == nil {
			if prior.RequestHash != requestHash {
				failure(c, http.StatusConflict, 47002, "Idempotency-Key was already used with different input", nil)
				return
			}
			c.Data(prior.Status, "application/json; charset=utf-8", prior.Response)
			return
		}
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			failure(c, http.StatusConflict, 47002, "request is already being processed", nil)
			return
		}
		failure(c, http.StatusInternalServerError, 50001, "request could not be completed", nil)
		return
	}
	data, apiErr := operation(tx)
	if apiErr != nil {
		tx.Rollback()
		failure(c, apiErr.status, apiErr.code, apiErr.msg, apiErr.data)
		return
	}
	response, err := json.Marshal(envelope{Code: 200, Msg: "success", Data: data})
	if err != nil {
		tx.Rollback()
		failure(c, http.StatusInternalServerError, 50001, "request could not be completed", nil)
		return
	}
	if err := tx.Model(&record).Updates(map[string]any{"response": response, "status": http.StatusOK}).Error; err != nil {
		tx.Rollback()
		failure(c, http.StatusInternalServerError, 50001, "request could not be completed", nil)
		return
	}
	if err := tx.Commit().Error; err != nil {
		failure(c, http.StatusInternalServerError, 50001, "request could not be completed", nil)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", bytes.TrimSpace(response))
}

func bind(c *gin.Context, target any) bool {
	_, ok := readBody(c, target)
	return ok
}
