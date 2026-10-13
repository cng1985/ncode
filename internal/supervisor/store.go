package supervisor

import (
	"context"

	"github.com/cng1985/ncode/internal/model"
	"gorm.io/gorm"
)

const maxLogsPerService = 1000

type GormStore struct{ db *gorm.DB }

func NewGormStore(db *gorm.DB) Store { return &GormStore{db: db} }

func (s *GormStore) AppendLog(ctx context.Context, entry LogEntry) error {
	entry.Message = trimMessage(entry.Message)
	if entry.Stream != "stdout" && entry.Stream != "stderr" && entry.Stream != "system" {
		entry.Stream = "system"
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := model.ServiceLog{ServiceID: entry.ServiceID, Stream: entry.Stream, Message: entry.Message}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.ServiceLog{}).Where("service_id = ?", entry.ServiceID).Count(&count).Error; err != nil {
			return err
		}
		if count <= maxLogsPerService {
			return nil
		}
		var cutoff model.ServiceLog
		if err := tx.Where("service_id = ?", entry.ServiceID).Order("id DESC").Offset(maxLogsPerService - 1).Limit(1).Find(&cutoff).Error; err != nil || cutoff.ID == 0 {
			return err
		}
		return tx.Where("service_id = ? AND id < ?", entry.ServiceID, cutoff.ID).Delete(&model.ServiceLog{}).Error
	})
}

func (s *GormStore) UpdateRuntime(ctx context.Context, serviceID int64, state RuntimeState) error {
	updates := map[string]any{
		"state":   state.State,
		"p_id":    state.PID,
		"version": gorm.Expr("version + 1"),
	}
	if state.ClearExit {
		updates["exit_code"] = gorm.Expr("NULL")
	} else if state.ExitCode != nil {
		updates["exit_code"] = *state.ExitCode
	}
	if state.StartedAt != nil {
		updates["started_at"] = *state.StartedAt
	}
	if state.ClearStopped {
		updates["stopped_at"] = gorm.Expr("NULL")
	} else if state.StoppedAt != nil {
		updates["stopped_at"] = *state.StoppedAt
	}
	result := s.db.WithContext(ctx).Model(&model.SystemService{}).Where("id = ?", serviceID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *GormStore) ListRunning(ctx context.Context) ([]RunningRef, error) {
	var services []model.SystemService
	if err := s.db.WithContext(ctx).Where("state = ?", model.ServiceRunning).Find(&services).Error; err != nil {
		return nil, err
	}
	refs := make([]RunningRef, 0, len(services))
	for _, service := range services {
		refs = append(refs, RunningRef{ID: service.ID, PID: service.PID})
	}
	return refs, nil
}
