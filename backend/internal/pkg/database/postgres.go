// Package database owns the connections to the three servers this system needs:
// Postgres for application data, MySQL for the FreeRADIUS schema (ADR 0002) and
// Redis for background jobs. Each connection also contributes a health checker
// so the API can report which dependency is down.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.uber.org/fx"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

// Postgres is the application database connection. It is a distinct type so Fx
// cannot confuse it with the FreeRADIUS *sql.DB.
type Postgres struct {
	Gorm *gorm.DB
	SQL  *sql.DB
}

// NewPostgres opens the application database and closes it on shutdown.
func NewPostgres(lc fx.Lifecycle, cfg *config.Config, logger *zap.Logger) (*Postgres, error) {
	db, err := gorm.Open(postgres.Open(cfg.Postgres.DSN()), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
		// GORM pings during Open by default, which would abort startup when
		// Postgres is down. The explicit ping below decides what to log, and
		// the health endpoint decides what to report.
		DisableAutomaticPing: true,
	})
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("postgres handle: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.Postgres.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.Postgres.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.Postgres.ConnMaxLifetime)

	// A failed first ping is reported by the health endpoint instead of
	// blocking startup: an operator needs the API up to see what is down.
	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		logger.Warn("postgres not reachable at startup", zap.Error(err))
	}

	lc.Append(fx.Hook{
		OnStop: func(_ context.Context) error {
			logger.Info("closing postgres")
			return sqlDB.Close()
		},
	})

	return &Postgres{Gorm: db, SQL: sqlDB}, nil
}
