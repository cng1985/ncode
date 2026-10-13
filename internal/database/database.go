package database

import (
	"context"
	"fmt"
	"time"

	"github.com/cng1985/ncode/internal/config"
	"github.com/cng1985/ncode/internal/model"
	"go.uber.org/fx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var Module = fx.Module(
	"database",
	fx.Provide(Open),
	fx.Invoke(registerMigrations),
)

func Open(lifecycle fx.Lifecycle, cfg config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Error),
	})
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get PostgreSQL connection pool: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}

	lifecycle.Append(fx.Hook{
		OnStop: func(context.Context) error {
			return sqlDB.Close()
		},
	})
	return db, nil
}

func registerMigrations(lifecycle fx.Lifecycle, db *gorm.DB, cfg config.Config) {
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			db = db.WithContext(ctx)
			if err := Migrate(db); err != nil {
				return fmt.Errorf("run database migrations: %w", err)
			}
			if cfg.SeedDemo {
				if err := model.SeedDemo(db); err != nil {
					return fmt.Errorf("seed demonstration data: %w", err)
				}
			}
			return nil
		},
	})
}

// Migrate applies the current schema and removes the pre-workspace global
// project slug index. Slugs are unique inside a workspace.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&model.User{},
		&model.Tenant{},
		&model.Membership{},
		&model.WorkspaceInvitation{},
		&model.EmailVerification{},
		&model.Session{},
		&model.Project{},
		&model.Repository{},
		&model.WorkItem{},
		&model.AcceptanceCriterion{},
		&model.Activity{},
		&model.IdempotencyRecord{},
	); err != nil {
		return err
	}
	if db.Migrator().HasIndex(&model.Project{}, "idx_projects_slug") {
		if err := db.Migrator().DropIndex(&model.Project{}, "idx_projects_slug"); err != nil {
			return fmt.Errorf("remove legacy global project slug index: %w", err)
		}
	}
	return nil
}
