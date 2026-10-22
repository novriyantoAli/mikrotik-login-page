// Package service holds the Package catalog rules: the starter catalog the
// deployment seeds and the mapping from stored values to the Login Page's
// frozen display contract.
package service

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/repository"
)

// Service is the catalog's public surface. It is read-only for users; Seed is
// the deployment path that fills the starter catalog (ADR 0012).
type Service interface {
	// ListPackages returns the catalog in the Login Page's contract shape.
	ListPackages(ctx context.Context) ([]dto.PackageResponse, error)
	// Seed inserts every starter Package that is not in the catalog yet and
	// reports how many rows it added. Running it twice adds nothing.
	Seed(ctx context.Context) (int64, error)
}

type service struct {
	repo   repository.Repository
	logger *zap.Logger
}

// NewService builds the catalog service; Fx injects the repository.
func NewService(repo repository.Repository, logger *zap.Logger) Service {
	return &service{repo: repo, logger: logger}
}

func (s *service) ListPackages(ctx context.Context) ([]dto.PackageResponse, error) {
	packages, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
	}

	responses := make([]dto.PackageResponse, 0, len(packages))
	for _, pkg := range packages {
		responses = append(responses, entityToResponse(pkg))
	}
	return responses, nil
}

func (s *service) Seed(ctx context.Context) (int64, error) {
	inserted, err := s.repo.InsertMissing(ctx, StarterCatalog())
	if err != nil {
		return 0, fmt.Errorf("seed package catalog: %w", err)
	}
	s.logger.Info("package catalog seeded",
		zap.Int("catalog_size", len(StarterCatalog())),
		zap.Int64("inserted", inserted),
	)
	return inserted, nil
}

// entityToResponse maps the typed row onto the frozen display contract. The
// Login Page renders these strings unchanged, so the formatting lives here and
// not in the page.
func entityToResponse(pkg entity.Package) dto.PackageResponse {
	return dto.PackageResponse{
		ID:           pkg.ID,
		Name:         pkg.Name,
		Price:        FormatRupiah(pkg.PriceIDR),
		ActivePeriod: FormatActivePeriod(pkg.DurationMinutes),
		SpeedLimit:   FormatSpeedLimit(pkg.RateLimit),
	}
}
