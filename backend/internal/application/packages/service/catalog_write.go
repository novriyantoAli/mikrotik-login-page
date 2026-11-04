// This file is the catalog's write surface: the rules the panel's forms call the
// service for, each one leaving a single audit row in the caller's transaction
// (ADR 0025, ADR 0027).
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
)

// Errors the catalog's write path reports. They are sentinels so the transport can
// answer 409/422 without matching on message text, while the message stays what an
// operator reads.
var (
	// ErrPackageInvalid wraps every field rule; a *FieldError names which one.
	ErrPackageInvalid = errors.New("package is not valid")
	// ErrPackageCodeTaken means another Package already holds the code.
	ErrPackageCodeTaken = errors.New("package code is already in use")
	// ErrPackageCodeImmutable means an edit tried to rename the seed's identity.
	ErrPackageCodeImmutable = errors.New("package code cannot be changed")
	// ErrPackageVersionConflict means the edit was based on a row that has since
	// changed; the *VersionConflictError carries the current one.
	ErrPackageVersionConflict = errors.New("package was changed by someone else")
	// ErrLastActivePackage means the deactivation would leave nothing on sale.
	ErrLastActivePackage = errors.New("the last active package cannot be deactivated")
	// ErrActorRequired means a write arrived without the admin who performed it.
	// Refusing is the point: an audited action with no actor is a trail row that
	// cannot answer the question it exists for (ADR 0027 §3).
	ErrActorRequired = errors.New("an audited action needs its actor")
)

// VersionConflictError is the catalog's refusal of a stale edit. Current is the row
// as the database has it now, so the panel can show what changed instead of
// answering a bare "conflict" (ADR 0025 §7).
type VersionConflictError struct {
	Current dto.AdminPackageResponse
}

// Error names the version the row is at, for the log.
func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("package %d is at version %d", e.Current.ID, e.Current.Version)
}

// Is makes errors.Is(err, ErrPackageVersionConflict) true.
func (e *VersionConflictError) Is(target error) bool { return target == ErrPackageVersionConflict }

func (s *service) CreatePackage(
	ctx context.Context,
	actor audit.Actor,
	request *dto.CreatePackageRequest,
) (*dto.AdminPackageResponse, error) {
	if err := requireActor(actor); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, &FieldError{Field: "request", Reason: "Permintaan tidak lengkap."}
	}

	if err := validateCode(request.Code); err != nil {
		return nil, err
	}
	name, err := validateName(request.Name)
	if err != nil {
		return nil, err
	}
	if err := validatePrice(request.PriceIDR); err != nil {
		return nil, err
	}
	if err := validateDuration(request.DurationMinutes); err != nil {
		return nil, err
	}
	if err := validateRateLimit(request.RateLimit); err != nil {
		return nil, err
	}

	// A new Package is on sale from the start and at its first revision, spelled
	// out rather than left to the column defaults (ADR 0025).
	pkg := &entity.Package{
		Code:            request.Code,
		Name:            name,
		PriceIDR:        request.PriceIDR,
		DurationMinutes: request.DurationMinutes,
		RateLimit:       request.RateLimit,
		IsActive:        true,
		Version:         1,
	}

	if err := s.repo.Create(ctx, pkg, packageCreateEntry(actor, *pkg)); err != nil {
		if errors.Is(err, repository.ErrDuplicateCode) {
			return nil, fmt.Errorf("%w: %s", ErrPackageCodeTaken, pkg.Code)
		}
		return nil, err
	}

	s.logger.Info("package created",
		zap.Uint("package_id", pkg.ID),
		zap.String("code", pkg.Code),
		zap.String("actor", actor.Email),
	)
	response := adminEntityToResponse(*pkg)
	return &response, nil
}

func (s *service) UpdatePackage(
	ctx context.Context,
	actor audit.Actor,
	id uint,
	request *dto.UpdatePackageRequest,
) (*dto.AdminPackageResponse, error) {
	if err := requireActor(actor); err != nil {
		return nil, err
	}
	if request == nil || request.Version <= 0 {
		return nil, &FieldError{Field: "version", Reason: "Versi paket wajib disertakan."}
	}

	// Code is the seed's identity. The read is what makes "same code" a no-op and
	// "different code" a refusal instead of a silent rename (ADR 0025 §5).
	current, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrPackageNotFound) {
			return nil, ErrPackageNotFound
		}
		return nil, err
	}
	if request.Code != "" && request.Code != current.Code {
		return nil, fmt.Errorf("%w: %s", ErrPackageCodeImmutable, current.Code)
	}

	name, err := validateName(request.Name)
	if err != nil {
		return nil, err
	}
	if err := validatePrice(request.PriceIDR); err != nil {
		return nil, err
	}
	if err := validateDuration(request.DurationMinutes); err != nil {
		return nil, err
	}
	if err := validateRateLimit(request.RateLimit); err != nil {
		return nil, err
	}

	edited := entity.Package{
		ID:              id,
		Code:            current.Code,
		Name:            name,
		PriceIDR:        request.PriceIDR,
		DurationMinutes: request.DurationMinutes,
		RateLimit:       request.RateLimit,
	}
	updated, err := s.repo.Update(ctx, &edited, request.Version, packageUpdateEntry(actor, edited))
	if err != nil {
		return nil, mapWriteError(err)
	}

	s.logger.Info("package updated",
		zap.Uint("package_id", updated.ID),
		zap.Int("version", updated.Version),
		zap.String("actor", actor.Email),
	)
	response := adminEntityToResponse(*updated)
	return &response, nil
}

func (s *service) SetActive(
	ctx context.Context,
	actor audit.Actor,
	id uint,
	request *dto.SetActiveRequest,
) (*dto.AdminPackageResponse, error) {
	if err := requireActor(actor); err != nil {
		return nil, err
	}
	if request == nil || request.Active == nil {
		return nil, &FieldError{Field: "active", Reason: "Status aktif wajib dikirim."}
	}

	updated, err := s.repo.SetActive(ctx, id, *request.Active, packageActiveEntry(actor, *request.Active))
	if err != nil {
		return nil, mapWriteError(err)
	}

	s.logger.Info("package active state set",
		zap.Uint("package_id", updated.ID),
		zap.Bool("is_active", updated.IsActive),
		zap.String("actor", actor.Email),
	)
	response := adminEntityToResponse(*updated)
	return &response, nil
}

// mapWriteError translates the repository's sentinels into the service's, so a
// transport never depends on the persistence layer's error values. Repository
// failures that are not refusals pass through already wrapped.
func mapWriteError(err error) error {
	var conflict *repository.VersionConflictError
	switch {
	case errors.As(err, &conflict):
		return &VersionConflictError{Current: adminEntityToResponse(conflict.Current)}
	case errors.Is(err, repository.ErrPackageNotFound):
		return ErrPackageNotFound
	case errors.Is(err, repository.ErrLastActivePackage):
		return ErrLastActivePackage
	default:
		return err
	}
}

// requireActor refuses an audited write with no admin behind it. The guard already
// rejects such a request; this is the service's own boundary, because a rule only
// the transport knows is a rule the next transport forgets.
func requireActor(actor audit.Actor) error {
	if actor.ID == 0 || strings.TrimSpace(actor.Email) == "" {
		return ErrActorRequired
	}
	return nil
}

// packageCreateEntry is the trail fact of a new Package: the values it was created
// with, never a credential (there are none in this domain).
func packageCreateEntry(actor audit.Actor, pkg entity.Package) audit.Entry {
	return audit.Entry{
		Actor:      actor,
		Action:     audit.ActionPackageCreate,
		TargetType: audit.TargetPackage,
		Payload: map[string]any{
			// A create has no before state, so the fact carries only the after side
			// the update and activation rows also use (ADR 0027 §3).
			"after": map[string]any{
				"code":             pkg.Code,
				"name":             pkg.Name,
				"price_idr":        pkg.PriceIDR,
				"duration_minutes": pkg.DurationMinutes,
				"rate_limit":       pkg.RateLimit,
				"is_active":        pkg.IsActive,
				"version":          pkg.Version,
			},
		},
	}
}

// packageUpdateEntry is the trail fact of an edit. It is built once the repository
// has the row a moment before the UPDATE, so "before" is what the database held,
// not what the request claimed.
func packageUpdateEntry(actor audit.Actor, pkg entity.Package) repository.EntryBuilder {
	return func(before entity.Package) audit.Entry {
		after := pkg
		after.Version = before.Version + 1
		return audit.Entry{
			Actor:      actor,
			Action:     audit.ActionPackageUpdate,
			TargetType: audit.TargetPackage,
			Payload:    packageUpdatePayload(before, after),
		}
	}
}

// packageActiveEntry names the act after the requested state, so the trail reads
// "activate" or "deactivate" rather than a generic update (ADR 0027 §3).
func packageActiveEntry(actor audit.Actor, active bool) repository.EntryBuilder {
	return func(before entity.Package) audit.Entry {
		action := audit.ActionPackageActivate
		if !active {
			action = audit.ActionPackageDeactivate
		}
		return audit.Entry{
			Actor:      actor,
			Action:     action,
			TargetType: audit.TargetPackage,
			Payload: map[string]any{
				"is_active": beforeAfter(before.IsActive, active),
				"version":   beforeAfter(before.Version, before.Version+1),
			},
		}
	}
}

// packageUpdatePayload records only the fields that actually changed, each as
// before/after, plus the version every write moves (ADR 0027 §3).
func packageUpdatePayload(before, after entity.Package) map[string]any {
	payload := map[string]any{"version": beforeAfter(before.Version, after.Version)}
	if before.Name != after.Name {
		payload["name"] = beforeAfter(before.Name, after.Name)
	}
	if before.PriceIDR != after.PriceIDR {
		payload["price_idr"] = beforeAfter(before.PriceIDR, after.PriceIDR)
	}
	if before.DurationMinutes != after.DurationMinutes {
		payload["duration_minutes"] = beforeAfter(before.DurationMinutes, after.DurationMinutes)
	}
	if before.RateLimit != after.RateLimit {
		payload["rate_limit"] = beforeAfter(before.RateLimit, after.RateLimit)
	}
	return payload
}

// beforeAfter is the shape every audited catalog field uses, so a reader can answer
// "what changed" without a second table.
func beforeAfter(before, after any) map[string]any {
	return map[string]any{"before": before, "after": after}
}
