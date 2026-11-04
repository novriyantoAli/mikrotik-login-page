// This file is the catalog's write edge: one statement per intent, each wrapped in
// the caller's transaction together with its audit row, so a change without a
// witness and a witness without a change are both unrepresentable (ADR 0027 §2).
package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
)

func (r *packageRepository) Create(ctx context.Context, pkg *entity.Package, entry audit.Entry) error {
	err := database.WithinTx(ctx, r.db, func(tx database.Tx) error {
		handle := tx.DB()
		if err := handle.Create(pkg).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				// Two admins creating one code is a conflict the service answers,
				// not a broken database.
				return ErrDuplicateCode
			}
			r.logger.Error("insert package", zap.String("code", pkg.Code), zap.Error(err))
			return fmt.Errorf("insert package %q: %w", pkg.Code, err)
		}

		// The row's identity only exists now, so the fact is completed here rather
		// than in the service that did not know it yet.
		recorded := entry.WithTarget(audit.TargetPackage, uintString(pkg.ID))
		return r.recorder.Record(ctx, tx, recorded)
	})
	if err != nil {
		return fmt.Errorf("create package %q: %w", pkg.Code, err)
	}
	return nil
}

func (r *packageRepository) Update(
	ctx context.Context,
	pkg *entity.Package,
	expectedVersion int,
	entry EntryBuilder,
) (*entity.Package, error) {
	var updated entity.Package

	err := database.WithinTx(ctx, r.db, func(tx database.Tx) error {
		handle := tx.DB()

		before, err := readForUpdate(handle, pkg.ID)
		if err != nil {
			return err
		}
		// The row is locked, so comparing before the write is enough: a concurrent
		// edit either committed already (and this answers 409) or waits here.
		if before.Version != expectedVersion {
			return &VersionConflictError{Current: *before}
		}

		after, err := r.applyVersionedUpdate(handle, pkg.ID, expectedVersion, map[string]any{
			"name":             pkg.Name,
			"price_idr":        pkg.PriceIDR,
			"duration_minutes": pkg.DurationMinutes,
			"rate_limit":       pkg.RateLimit,
		})
		if err != nil {
			return err
		}
		updated = *after

		recorded := entry(*before).WithTarget(audit.TargetPackage, uintString(pkg.ID))
		return r.recorder.Record(ctx, tx, recorded)
	})
	if err != nil {
		if errors.Is(err, ErrPackageNotFound) || errors.Is(err, ErrVersionConflict) {
			return nil, err
		}
		return nil, fmt.Errorf("update package %d: %w", pkg.ID, err)
	}
	return &updated, nil
}

func (r *packageRepository) SetActive(
	ctx context.Context,
	id uint,
	active bool,
	entry EntryBuilder,
) (*entity.Package, error) {
	var updated entity.Package

	err := database.WithinTx(ctx, r.db, func(tx database.Tx) error {
		handle := tx.DB()

		activeIDs, err := lockActivePackages(handle)
		if err != nil {
			return err
		}

		before, err := readForUpdate(handle, id)
		if err != nil {
			return err
		}
		if before.IsActive == active {
			// A repeated request is not a write: it neither moves the version nor
			// leaves a trail row for an act that changed nothing.
			updated = *before
			return nil
		}
		// The check belongs inside this transaction: an intermittent advisory
		// count in the service could let two requests retire the last two Packages
		// at the same time and leave the public catalog empty (ADR 0025 §4).
		if !active && len(activeIDs) <= 1 {
			return ErrLastActivePackage
		}

		after, err := r.applyVersionedUpdate(handle, id, before.Version, map[string]any{"is_active": active})
		if err != nil {
			return err
		}
		updated = *after

		recorded := entry(*before).WithTarget(audit.TargetPackage, uintString(id))
		return r.recorder.Record(ctx, tx, recorded)
	})
	if err != nil {
		if errors.Is(err, ErrPackageNotFound) ||
			errors.Is(err, ErrVersionConflict) ||
			errors.Is(err, ErrLastActivePackage) {
			return nil, err
		}
		return nil, fmt.Errorf("set package %d active: %w", id, err)
	}
	return &updated, nil
}

// applyVersionedUpdate runs one conditional UPDATE at the version the caller read
// and returns the row as it is afterwards. A zero row count means the row moved
// between the lock and the statement, which the caller answers with the current
// row rather than overwriting it (ADR 0025 §7).
func (r *packageRepository) applyVersionedUpdate(
	handle *gorm.DB,
	id uint,
	expectedVersion int,
	changes map[string]any,
) (*entity.Package, error) {
	changes["version"] = expectedVersion + 1

	result := handle.Model(&entity.Package{}).
		Where("id = ? AND version = ?", id, expectedVersion).
		Updates(changes)
	if result.Error != nil {
		r.logger.Error("update package", zap.Uint("package_id", id), zap.Error(result.Error))
		return nil, fmt.Errorf("update package %d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		current, err := readForUpdate(handle, id)
		if err != nil {
			return nil, err
		}
		return nil, &VersionConflictError{Current: *current}
	}

	var updated entity.Package
	if err := handle.Where("id = ?", id).First(&updated).Error; err != nil {
		r.logger.Error("read updated package", zap.Uint("package_id", id), zap.Error(err))
		return nil, fmt.Errorf("read updated package %d: %w", id, err)
	}
	return &updated, nil
}

// lockActivePackages takes a row lock on every Package still on sale, in one
// order, so concurrent retirements queue instead of each seeing "one other is
// active" and both committing.
func lockActivePackages(handle *gorm.DB) ([]uint, error) {
	var ids []uint

	err := handle.Model(&entity.Package{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("is_active = ?", true).
		Order("id ASC").
		Pluck("id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("lock active packages: %w", err)
	}
	return ids, nil
}

// readForUpdate loads one Package and locks its row, so the version a write was
// decided on cannot move between the read and the statement it justifies.
func readForUpdate(handle *gorm.DB, id uint) (*entity.Package, error) {
	var pkg entity.Package

	err := handle.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&pkg).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, ErrPackageNotFound
	case err != nil:
		return nil, fmt.Errorf("read package %d for update: %w", id, err)
	}
	return &pkg, nil
}

func uintString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
