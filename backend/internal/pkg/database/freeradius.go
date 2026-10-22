package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql" // registers the mysql driver
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

// FreeRADIUS is the FreeRADIUS MySQL connection. Its own type keeps it distinct
// from the Postgres handle in the Fx graph.
type FreeRADIUS struct {
	DB *sql.DB
}

// NewFreeRADIUS opens the FreeRADIUS database. The backend writes radcheck and
// radreply rows here (ADR 0001); it never owns this schema.
func NewFreeRADIUS(lc fx.Lifecycle, cfg *config.Config, logger *zap.Logger) (*FreeRADIUS, error) {
	db, err := sql.Open("mysql", cfg.FreeRADIUSMySQL.DSN())
	if err != nil {
		return nil, fmt.Errorf("open freeradius mysql: %w", err)
	}

	db.SetMaxOpenConns(cfg.FreeRADIUSMySQL.MaxOpenConns)
	db.SetMaxIdleConns(cfg.FreeRADIUSMySQL.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.FreeRADIUSMySQL.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		logger.Warn("freeradius mysql not reachable at startup", zap.Error(err))
	}

	lc.Append(fx.Hook{
		OnStop: func(_ context.Context) error {
			logger.Info("closing freeradius mysql")
			return db.Close()
		},
	})

	return &FreeRADIUS{DB: db}, nil
}
