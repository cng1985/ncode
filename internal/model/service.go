package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	ServiceStopped = 0
	ServiceRunning = 1
	ServiceFailed  = 2
)

const (
	ServiceKindBuiltin = "builtin"
	ServiceKindProcess = "process"
)

type SystemService struct {
	ID          int64  `gorm:"primaryKey"`
	Name        string `gorm:"size:120;not null"`
	Code        string `gorm:"size:80;not null;uniqueIndex"`
	Description string `gorm:"size:500;not null;default:''"`
	Kind        string `gorm:"size:16;not null;index"`
	Command     string `gorm:"size:200;not null"`
	Args        string `gorm:"type:text;not null;default:'[]'"`
	WorkDir     string `gorm:"size:500;not null;default:''"`
	State       int    `gorm:"not null;default:0;index"`
	PID         int    `gorm:"column:p_id;not null;default:0"`
	ExitCode    *int
	Version     int `gorm:"not null;default:1"`
	StartedAt   *time.Time
	StoppedAt   *time.Time
	CreatedAt   time.Time `gorm:"not null"`
	UpdatedAt   time.Time `gorm:"not null"`
}

type ServiceLog struct {
	ID        int64     `gorm:"primaryKey"`
	ServiceID int64     `gorm:"not null;index"`
	Stream    string    `gorm:"size:16;not null"`
	Message   string    `gorm:"type:text;not null"`
	CreatedAt time.Time `gorm:"not null;index"`
}

func (svc *SystemService) BeforeCreate(*gorm.DB) error {
	if svc.Version == 0 {
		svc.Version = 1
	}
	if svc.Args == "" {
		svc.Args = "[]"
	}
	if svc.Kind == "" {
		svc.Kind = ServiceKindProcess
	}
	return nil
}

func SeedServices(db *gorm.DB) error {
	var count int64
	if err := db.Model(&SystemService{}).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	services := []SystemService{
		{
			Name:        "演示心跳",
			Code:        "demo-heartbeat",
			Description: "按固定间隔写入心跳日志，用来确认服务启动、停止和日志查看。",
			Kind:        ServiceKindBuiltin,
			Command:     "heartbeat",
			Args:        "[]",
			State:       ServiceStopped,
			Version:     1,
		},
		{
			Name:        "演示短任务",
			Code:        "demo-once",
			Description: "启动后写入开始和完成日志，然后正常退出。",
			Kind:        ServiceKindBuiltin,
			Command:     "once",
			Args:        "[]",
			State:       ServiceStopped,
			Version:     1,
		},
	}
	return db.Create(&services).Error
}
