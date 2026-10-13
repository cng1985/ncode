package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const MaxSafeID int64 = 9007199254740991

type Project struct {
	ID         int64     `gorm:"primaryKey"`
	Name       string    `gorm:"size:120;not null"`
	Slug       string    `gorm:"size:80;not null;uniqueIndex"`
	Goal       string    `gorm:"size:500;not null;default:''"`
	Type       string    `gorm:"size:24;not null;default:'product';index"`
	Visibility string    `gorm:"size:16;not null;default:'private'"`
	State      int       `gorm:"not null;default:1;index"`
	OwnerName  string    `gorm:"size:80;not null;default:'我'"`
	CreatedAt  time.Time `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"not null"`
}

type WorkItem struct {
	ID               int64                 `gorm:"primaryKey"`
	ProjectID        int64                 `gorm:"not null;index"`
	Code             string                `gorm:"size:24;not null;uniqueIndex"`
	Title            string                `gorm:"size:180;not null"`
	Description      string                `gorm:"type:text;not null;default:''"`
	Type             string                `gorm:"size:24;not null;default:'TASK';index"`
	Priority         string                `gorm:"size:16;not null;default:'MEDIUM';index"`
	State            int                   `gorm:"not null;default:0;index"`
	AcceptanceState  string                `gorm:"size:16;not null;default:'NONE';index"`
	OwnerName        string                `gorm:"size:80;not null;default:'';index"`
	DueDate          LocalDate             `gorm:"type:date"`
	Version          int                   `gorm:"not null;default:1"`
	ChecklistVersion int                   `gorm:"not null;default:1"`
	Criteria         []AcceptanceCriterion `gorm:"foreignKey:WorkItemID;constraint:OnDelete:CASCADE"`
	Project          Project               `gorm:"foreignKey:ProjectID"`
	CreatedAt        time.Time             `gorm:"not null"`
	UpdatedAt        time.Time             `gorm:"not null"`
}

type AcceptanceCriterion struct {
	ID         int64     `gorm:"primaryKey"`
	WorkItemID int64     `gorm:"not null;index"`
	Text       string    `gorm:"size:240;not null"`
	Checked    bool      `gorm:"not null;default:false"`
	Position   int       `gorm:"not null;default:0"`
	CreatedAt  time.Time `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"not null"`
}

type Activity struct {
	ID         int64     `gorm:"primaryKey"`
	ProjectID  int64     `gorm:"not null;index"`
	WorkItemID int64     `gorm:"not null;index"`
	Actor      string    `gorm:"size:80;not null"`
	Action     string    `gorm:"size:40;not null"`
	OldValue   string    `gorm:"type:text;not null;default:''"`
	NewValue   string    `gorm:"type:text;not null;default:''"`
	Version    int       `gorm:"not null"`
	CreatedAt  time.Time `gorm:"not null;index"`
}

type IdempotencyRecord struct {
	ID          int64     `gorm:"primaryKey"`
	Key         string    `gorm:"size:120;not null;uniqueIndex:idx_idempotency_action"`
	Action      string    `gorm:"size:80;not null;uniqueIndex:idx_idempotency_action"`
	RequestHash string    `gorm:"size:64;not null"`
	Status      int       `gorm:"not null;default:202"`
	Response    []byte    `gorm:"type:jsonb;not null"`
	CreatedAt   time.Time `gorm:"not null;index"`
}

func RequestHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func SeedDemo(db *gorm.DB) error {
	var count int64
	if err := db.Model(&Project{}).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	now := time.Now().In(time.FixedZone("CST", 8*60*60))
	projects := []Project{
		{Name: "NCode 官网", Slug: "ncode-website", Goal: "重构网站前端架构，提升性能与可维护性。", Type: "product", Visibility: "private", State: 1, OwnerName: "我"},
		{Name: "在线评测系统", Slug: "online-evaluation", Goal: "优化评测任务的调度、隔离与反馈。", Type: "product", Visibility: "private", State: 1, OwnerName: "我"},
		{Name: "移动端应用", Slug: "mobile-app", Goal: "完成版本发布并跟进首批用户反馈。", Type: "product", Visibility: "private", State: 1, OwnerName: "我"},
	}
	if err := db.Create(&projects).Error; err != nil {
		return err
	}
	date := func(days int) LocalDate { return LocalDate(now.AddDate(0, 0, days).Format("2006-01-02")) }
	items := []WorkItem{
		{ProjectID: projects[0].ID, Code: "NC-241", Title: "实现首页开发者案例展示模块", Description: "完成首页案例列表与详情入口，适配桌面和移动屏幕。", Type: "TASK", Priority: "HIGH", State: 2, OwnerName: "张三", DueDate: date(2), Version: 1, ChecklistVersion: 1, Criteria: []AcceptanceCriterion{{Text: "案例列表在桌面端正常展示", Position: 0}, {Text: "窄屏布局无横向溢出", Position: 1}, {Text: "接口失败时显示可重试状态", Position: 2}}},
		{ProjectID: projects[1].ID, Code: "NC-318", Title: "优化评测任务调度性能", Description: "减少队列等待时间，并确保旧版本结果不会覆盖新提交。", Type: "TASK", Priority: "HIGH", State: 3, OwnerName: "李四", DueDate: date(3), Version: 1, ChecklistVersion: 1, Criteria: []AcceptanceCriterion{{Text: "固定提交版本生成评测结果", Position: 0}, {Text: "基础设施错误不计为错答", Position: 1}}},
		{ProjectID: projects[2].ID, Code: "NC-402", Title: "发布 1.2.0 版本并完成上线检查", Description: "核对发布清单、应用健康状态和回滚方案。", Type: "STORY", Priority: "MEDIUM", State: 3, AcceptanceState: "PENDING", OwnerName: "王五", DueDate: date(7), Version: 1, ChecklistVersion: 1, Criteria: []AcceptanceCriterion{{Text: "版本健康检查通过", Position: 0}, {Text: "回滚版本可用", Position: 1}, {Text: "发布记录完整", Position: 2}}},
	}
	for i := range items {
		if items[i].AcceptanceState == "" {
			items[i].AcceptanceState = "NONE"
		}
	}
	return db.Create(&items).Error
}

func (item *WorkItem) BeforeCreate(tx *gorm.DB) error {
	if item.Version == 0 {
		item.Version = 1
	}
	if item.ChecklistVersion == 0 {
		item.ChecklistVersion = 1
	}
	if item.AcceptanceState == "" {
		item.AcceptanceState = "NONE"
	}
	return nil
}

func SafeID(id int64) bool { return id > 0 && id <= MaxSafeID }

func CodeForWorkItem(id int64) string { return fmt.Sprintf("NC-%03d", id) }
