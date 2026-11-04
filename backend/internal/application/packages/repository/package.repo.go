// Package repository is the persistence edge of the Package domain. Reads serve
// both catalogs (the public one and the panel's); writes retire Packages instead
// of deleting them and leave one audit row per change in the same transaction
// (ADR 0025, ADR 0027).
package repository

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
)

// ErrPackageNotFound means the catalog holds no Package with that identity. It
// is a sentinel so a caller (checkout) can answer 400 instead of 500.
var ErrPackageNotFound = errors.New("package not found")

// ErrDuplicateCode means another Package already holds that code. The unique
// index is the authority, so the answer survives two admins creating the same
// product at once; the service turns it into a 409.
var ErrDuplicateCode = errors.New("package code already exists")

// ErrVersionConflict means the version a write presented is not the row's current
// one, so the edit would overwrite a change the admin never saw (ADR 0025 §7).
var ErrVersionConflict = errors.New("package version is stale")

// ErrLastActivePackage means a deactivation would leave the public catalog empty.
// The Login Page would render no Package and "Beli" would have no target, so the
// service refuses with a 409 instead (ADR 0025 §4).
var ErrLastActivePackage = errors.New("the last active package cannot be deactivated")

// VersionConflictError carries the row as the repository read it, so the transport
// can answer 409 with the current values instead of a bare refusal the panel would
// have to re-read blind.
type VersionConflictError struct {
	Current entity.Package
}

// Error names the two versions so the log answers the question the panel cannot.
func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("package %d is at version %d, not the one presented", e.Current.ID, e.Current.Version)
}

// Is makes errors.Is(err, ErrVersionConflict) true, so a caller can branch on the
// refusal without matching on message text.
func (e *VersionConflictError) Is(target error) bool { return target == ErrVersionConflict }

// EntryBuilder composes the audit fact once the repository has read the row it is
// about to change. The service owns what a trail row says; the repository owns
// when it is written, which is inside the same transaction (ADR 0027 §2).
type EntryBuilder func(before entity.Package) audit.Entry

// Repository reads and writes the Package catalog.
type Repository interface {
	// ListActive returns the Packages the hotspot still sells, in seed order
	// (id ascending). It is the public catalog's only read.
	ListActive(ctx context.Context) ([]entity.Package, error)
	// ListAll returns every Package, active or retired, for the panel's list.
	ListAll(ctx context.Context) ([]entity.Package, error)
	// GetByID returns one Package, or ErrPackageNotFound when the catalog has
	// no such row. Checkout reads the typed row (price, duration, rate limit)
	// instead of re-parsing the display strings of ListActive.
	GetByID(ctx context.Context, id uint) (*entity.Package, error)
	// InsertMissing inserts only the rows whose code is not in the catalog yet
	// and reports how many it added. Existing rows are left untouched, so an
	// operator's edit survives a later deployment (ADR 0012).
	InsertMissing(ctx context.Context, packages []entity.Package) (int64, error)

	// Create stores one Package and appends its trail row inside the same
	// transaction; a taken code is ErrDuplicateCode (ADR 0025, ADR 0027 §2).
	Create(ctx context.Context, pkg *entity.Package, entry audit.Entry) error
	// Update applies one edit only when the row is still at expectedVersion. A
	// stale version answers *VersionConflictError with the current row; the edit
	// and its trail row commit or roll back together.
	Update(ctx context.Context, pkg *entity.Package, expectedVersion int, entry EntryBuilder) (*entity.Package, error)
	// SetActive retires or revives one Package. Retiring the last active Package
	// is ErrLastActivePackage; the change and its trail row share a transaction.
	SetActive(ctx context.Context, id uint, active bool, entry EntryBuilder) (*entity.Package, error)
}

type packageRepository struct {
	db       *gorm.DB
	recorder audit.Recorder
	logger   *zap.Logger
}

// NewRepository builds the GORM-backed repository; Fx injects the handle, the
// audit port every recording domain depends on, and the logger.
func NewRepository(db *gorm.DB, recorder audit.Recorder, logger *zap.Logger) Repository {
	return &packageRepository{db: db, recorder: recorder, logger: logger}
}

func (r *packageRepository) ListActive(ctx context.Context) ([]entity.Package, error) {
	return r.list(ctx, true)
}

func (r *packageRepository) ListAll(ctx context.Context) ([]entity.Package, error) {
	return r.list(ctx, false)
}

// list is the one catalog read, shared by both lists so the ordering cannot drift
// between the public contract and the panel.
func (r *packageRepository) list(ctx context.Context, activeOnly bool) ([]entity.Package, error) {
	var packages []entity.Package

	query := r.db.WithContext(ctx).Order("id ASC")
	if activeOnly {
		query = query.Where("is_active = ?", true)
	}
	if err := query.Find(&packages).Error; err != nil {
		r.logger.Error("list packages", zap.Bool("active_only", activeOnly), zap.Error(err))
		return nil, fmt.Errorf("list packages: %w", err)
	}
	return packages, nil
}

func (r *packageRepository) GetByID(ctx context.Context, id uint) (*entity.Package, error) {
	var pkg entity.Package

	err := r.db.WithContext(ctx).Where("id = ?", id).First(&pkg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPackageNotFound
		}
		r.logger.Error("read package", zap.Uint("package_id", id), zap.Error(err))
		return nil, fmt.Errorf("read package %d: %w", id, err)
	}
	return &pkg, nil
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
