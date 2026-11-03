package service

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	packageservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
)

// SnapshotBackfiller fills the catalog terms on Transactions that predate the
// snapshot columns. It is the deployment's one-shot path, run by `migrate`
// before the API starts (ADR 0026).
type SnapshotBackfiller interface {
	// BackfillSnapshots fills every pending or paid Transaction whose duration or
	// rate limit is missing, and reports how many rows it wrote. A provisioned row
	// is never touched: its effect is already in FreeRADIUS. Running it twice
	// writes nothing the second time.
	BackfillSnapshots(ctx context.Context) (int64, error)
}

// backfillBatch bounds one query so a large legacy table is chipped away rather
// than loading every row into memory inside the deployment's migration budget.
const backfillBatch = 200

type snapshotBackfiller struct {
	repo    repository.Repository
	catalog packageservice.Service
	logger  *zap.Logger
}

// NewSnapshotBackfiller builds the backfiller; Fx injects the Transaction
// repository and the catalog service it copies terms from.
func NewSnapshotBackfiller(
	repo repository.Repository,
	catalog packageservice.Service,
	logger *zap.Logger,
) SnapshotBackfiller {
	return &snapshotBackfiller{repo: repo, catalog: catalog, logger: logger}
}

func (b *snapshotBackfiller) BackfillSnapshots(ctx context.Context) (int64, error) {
	// One catalog read per Package, however many rows bought it.
	terms := make(map[uint]catalogTerms)
	filled := int64(0)
	afterID := uint(0)

	for {
		targets, err := b.repo.UnprovisionedWithoutSnapshot(ctx, afterID, backfillBatch)
		if err != nil {
			return filled, err
		}
		if len(targets) == 0 {
			return filled, nil
		}
		afterID = targets[len(targets)-1].ID

		for _, target := range targets {
			copied, ok := terms[target.PackageID]
			if !ok {
				copied, err = b.catalogTerms(ctx, target.PackageID)
				if err != nil {
					return filled, err
				}
				terms[target.PackageID] = copied
			}
			if copied.missing {
				// A row whose Package is gone cannot be backfilled, and inventing
				// terms for it would be worse than leaving the column empty. It is
				// logged and skipped, not a deployment blocker.
				continue
			}

			changed, err := b.repo.FillSnapshot(ctx, target.ID, copied.durationMinutes, copied.rateLimit)
			if err != nil {
				return filled, err
			}
			if changed {
				filled++
			}
		}
	}
}

// catalogTerms is the part of a Package the snapshot needs, plus whether the
// Package has disappeared from the catalog.
type catalogTerms struct {
	durationMinutes int
	rateLimit       string
	missing         bool
}

func (b *snapshotBackfiller) catalogTerms(ctx context.Context, packageID uint) (catalogTerms, error) {
	pkg, err := b.catalog.GetPackage(ctx, packageID)
	if err != nil {
		if errors.Is(err, packageservice.ErrPackageNotFound) {
			b.logger.Warn("transaction snapshot backfill skipped: Package is not in the catalog",
				zap.Uint("package_id", packageID),
			)
			return catalogTerms{missing: true}, nil
		}
		return catalogTerms{}, fmt.Errorf("read Package %d for snapshot backfill: %w", packageID, err)
	}
	return catalogTerms{
		durationMinutes: pkg.DurationMinutes,
		rateLimit:       pkg.RateLimit,
	}, nil
}
