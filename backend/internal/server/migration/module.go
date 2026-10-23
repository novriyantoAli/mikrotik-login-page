// Package migration is the deployment's one-shot schema and data step: it runs
// before the API starts, so no request ever sees a missing table or an empty
// catalog (ADR 0012).
package migration

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/fx"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction"
)

// upTimeout bounds one migration run; it is a process budget, not a deployment
// value, and is deliberately generous because AutoMigrate locks tables.
const upTimeout = 60 * time.Second

// Migrator applies the schema and the seed data this deployment owns.
type Migrator struct {
	db      *gorm.DB
	catalog service.Service
	logger  *zap.Logger
}

// NewMigrator builds the migrator; Fx injects the Postgres handle and the
// Package catalog service.
func NewMigrator(db *gorm.DB, catalog service.Service, logger *zap.Logger) *Migrator {
	return &Migrator{db: db, catalog: catalog, logger: logger}
}

// Up creates or updates every table the domains declare, then seeds the
// Package catalog. Both steps are idempotent: running the command again
// changes nothing and inserts no duplicate Package (ADR 0012).
func (m *Migrator) Up(ctx context.Context) error {
	if err := m.db.WithContext(ctx).AutoMigrate(migrations()...); err != nil {
		m.logger.Error("apply schema", zap.Error(err))
		return fmt.Errorf("apply schema: %w", err)
	}

	inserted, err := m.catalog.Seed(ctx)
	if err != nil {
		m.logger.Error("seed package catalog", zap.Error(err))
		return fmt.Errorf("seed package catalog: %w", err)
	}

	m.logger.Info("migration complete", zap.Int64("packages_inserted", inserted))
	return nil
}

// migrations collects every table the domains declare, so a new domain only has
// to add itself here.
func migrations() []any {
	tables := make([]any, 0, 5)
	tables = append(tables, packages.Migrations()...)
	tables = append(tables, auth.Migrations()...)
	tables = append(tables, notification.Migrations()...)
	tables = append(tables, transaction.Migrations()...)
	return tables
}

// Module runs the migration when the process starts.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(NewMigrator),
		fx.Invoke(runUp),
	)
}

func runUp(migrator *Migrator) error {
	ctx, cancel := context.WithTimeout(context.Background(), upTimeout)
	defer cancel()
	return migrator.Up(ctx)
}
