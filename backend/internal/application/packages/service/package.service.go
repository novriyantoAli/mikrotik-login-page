// Package service holds the Package catalog rules: the starter catalog the
// deployment seeds and the mapping from stored values to the Login Page's
// frozen display contract.
package service

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
)

// ErrPackageNotFound is the catalog's own sentinel for an unknown Package, so a
// caller in another domain never depends on the repository's error value.
var ErrPackageNotFound = errors.New("package not found")

// Service is the catalog's public surface. Users only ever read the active
// catalog; the panel's writes live here too, because the Package domain owns its
// rules (golden rule 2, ADR 0025).
type Service interface {
	// ListPackages returns the catalog the Login Page may sell, in the frozen
	// contract shape. Retired Packages are left out; the payload shape is
	// untouched (ADR 0025 §1).
	ListPackages(ctx context.Context) ([]dto.PackageResponse, error)
	// GetPackage returns the typed row another domain needs for a purchase:
	// PriceIDR, DurationMinutes and RateLimit live here, not in the display
	// contract. Entities may cross domain boundaries; they never cross HTTP.
	GetPackage(ctx context.Context, id uint) (*entity.Package, error)
	// Seed inserts every starter Package that is not in the catalog yet and
	// reports how many rows it added. Running it twice adds nothing.
	Seed(ctx context.Context) (int64, error)

	// ListAdminPackages returns every Package, retired ones included, in the
	// panel's typed shape with each row's version (ADR 0025 §1).
	ListAdminPackages(ctx context.Context) ([]dto.AdminPackageResponse, error)
	// CreatePackage adds one Package and leaves exactly one trail row for it, in
	// the same transaction (ADR 0027 §2). The actor is the signed-in admin.
	CreatePackage(
		ctx context.Context, actor audit.Actor, request *dto.CreatePackageRequest,
	) (*dto.AdminPackageResponse, error)
	// UpdatePackage applies an edit the panel read at request.Version. A stale
	// version answers *VersionConflictError carrying the row as it is now, and the
	// edit is refused rather than overwriting a change nobody saw (ADR 0025 §7).
	UpdatePackage(
		ctx context.Context, actor audit.Actor, id uint, request *dto.UpdatePackageRequest,
	) (*dto.AdminPackageResponse, error)
	// SetActive retires or revives one Package. Retiring the last active Package is
	// refused with ErrLastActivePackage (ADR 0025 §4).
	SetActive(
		ctx context.Context, actor audit.Actor, id uint, request *dto.SetActiveRequest,
	) (*dto.AdminPackageResponse, error)
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
	packages, err := s.repo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
	}

	responses := make([]dto.PackageResponse, 0, len(packages))
	for _, pkg := range packages {
		responses = append(responses, entityToResponse(pkg))
	}
	return responses, nil
}

func (s *service) ListAdminPackages(ctx context.Context) ([]dto.AdminPackageResponse, error) {
	packages, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
	}

	responses := make([]dto.AdminPackageResponse, 0, len(packages))
	for _, pkg := range packages {
		responses = append(responses, adminEntityToResponse(pkg))
	}
	return responses, nil
}

func (s *service) GetPackage(ctx context.Context, id uint) (*entity.Package, error) {
	pkg, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrPackageNotFound) {
			return nil, ErrPackageNotFound
		}
		return nil, fmt.Errorf("read package %d: %w", id, err)
	}
	return pkg, nil
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

// adminEntityToResponse maps the typed row onto the panel's contract. It is the
// same entity as the public one and deliberately a different shape: the panel
// edits money and minutes, and must see retired rows and their version.
func adminEntityToResponse(pkg entity.Package) dto.AdminPackageResponse {
	return dto.AdminPackageResponse{
		ID:              pkg.ID,
		Code:            pkg.Code,
		Name:            pkg.Name,
		PriceIDR:        pkg.PriceIDR,
		DurationMinutes: pkg.DurationMinutes,
		RateLimit:       pkg.RateLimit,
		IsActive:        pkg.IsActive,
		Version:         pkg.Version,
		CreatedAt:       pkg.CreatedAt,
		UpdatedAt:       pkg.UpdatedAt,
	}
}
