// Package repository is the persistence edge of the Package domain. It only
// reads data: the catalog is written by the deployment seed until the Admin
// Panel exists (ADR 0012), so there is no Update/Delete yet.
package repository

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
)

// Repository reads and seeds the Package catalog.
type Repository interface {
	// List returns the catalog in seed order (id ascending).
	List(ctx context.Context) ([]entity.Package, error)
	// InsertMissing inserts only the rows whose code is not in the catalog yet
	// and reports how many it added. Existing rows are left untouched, so an
	// operator's edit survives a later deployment (ADR 0012).
	InsertMissing(ctx context.Context, packages []entity.Package) (int64, error)
}

type packageRepository struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewRepository builds the GORM-backed repository; Fx injects the handle.
func NewRepository(db *gorm.DB, logger *zap.Logger) Repository {
	return &packageRepository{db: db, logger: logger}
}

func (r *packageRepository) List(ctx context.Context) ([]entity.Package, error) {
	var packages []entity.Package
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&packages).Error; err != nil {
		r.logger.Error("list packages", zap.Error(err))
		return nil, fmt.Errorf("list packages: %w", err)
	}
	return packages, nil
}

func (r *packageRepository) InsertMissing(ctx context.Context, packages []entity.Package) (int64, error) {
	if len(packages) == 0 {
		return 0, nil
	}

	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "code"}}, DoNothing: true}).
		CreateInBatches(&packages, len(packages))
	if result.Error != nil {
		r.logger.Error("seed packages", zap.Error(result.Error))
		return 0, fmt.Errorf("seed packages: %w", result.Error)
	}
	return result.RowsAffected, nil
}
