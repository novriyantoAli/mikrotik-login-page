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

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin"
	adminservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction"
	transactionservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
)

// upTimeout bounds one migration run; it is a process budget, not a deployment
// value, and is deliberately generous because AutoMigrate locks tables.
const upTimeout = 60 * time.Second

// Migrator applies the schema and the seed data this deployment owns.
type Migrator struct {
	db        *gorm.DB
	catalog   service.Service
	admins    adminservice.Service
	snapshots transactionservice.SnapshotBackfiller
	logger    *zap.Logger
}

// NewMigrator builds the migrator; Fx injects the Postgres handle, the Package
// catalog service, the admin bootstrap and the Transaction snapshot backfiller.
func NewMigrator(
	db *gorm.DB,
	catalog service.Service,
	admins adminservice.Service,
	snapshots transactionservice.SnapshotBackfiller,
	logger *zap.Logger,
) *Migrator {
	return &Migrator{db: db, catalog: catalog, admins: admins, snapshots: snapshots, logger: logger}
}

// Up creates or updates every table the domains declare, seeds the Package
// catalog, seeds the first admin owner, then backfills the catalog snapshot on
// pending or paid Transactions that predate it. Every step is idempotent: running
// the command again changes nothing, inserts no duplicate Package and never
// rewrites an existing admin password (ADR 0012, ADR 0024, ADR 0026).
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

	// The panel must have a way in after a fresh deploy, and a deploy must never
	// reset a password that already exists, so this step refuses to finish when
	// the table is empty and the bootstrap pair is missing (ADR 0024 §6).
	admins, err := m.admins.BootstrapOwner(ctx)
	if err != nil {
		m.logger.Error("seed first admin", zap.Error(err))
		return fmt.Errorf("seed first admin: %w", err)
	}

	// Filled after the seed, so a catalog this deployment just created is what
	// the pending and paid rows copy from (ADR 0026).
	backfilled, err := m.snapshots.BackfillSnapshots(ctx)
	if err != nil {
		m.logger.Error("backfill transaction snapshots", zap.Error(err))
		return fmt.Errorf("backfill transaction snapshots: %w", err)
	}

	m.logger.Info("migration complete",
		zap.Int64("packages_inserted", inserted),
		zap.Int64("admins_inserted", admins),
		zap.Int64("transactions_backfilled", backfilled),
	)
	return nil
}

// migrations collects every table the domains declare, so a new domain only has
// to add itself here.
func migrations() []any {
	tables := make([]any, 0, 8)
	tables = append(tables, packages.Migrations()...)
	tables = append(tables, admin.Migrations()...)
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
