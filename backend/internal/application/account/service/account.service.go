// Package service composes Account Status from the domains that already own its
// facts: the Transaction domain for the PIN and the history, the Package catalog
// for the active Package, and the provisioning port for the Active Period end.
// It owns no table on purpose — Phone Number is the only identity, and FreeRADIUS
// is the source of truth for credentials and expiry (PRD point 6, ADR 0017).
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/account/dto"
	packageservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
	transactionservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
)

// ErrPackageUnknown means the requested Package is not in the catalog, which the
// Extend preview reports as a bad request rather than an estimate.
var ErrPackageUnknown = errors.New("package not found")

// Settings are the Account Status knobs, kept local so the domain never depends
// on the deployment configuration struct.
type Settings struct {
	// ExpiryWarning is the window before the Active Period end that counts as
	// "almost over". It mirrors the Login Page's expiryWarningHours (ADR 0017).
	ExpiryWarning time.Duration
}

// Service is the Account Status read surface. Both methods authorize through the
// Phone Number the caller passes, and the HTTP handler only ever passes the
// session's (PRD point 14).
type Service interface {
	// Summary returns the active Package, the Active Period state and the current
	// PIN for one Phone Number.
	Summary(ctx context.Context, phoneNumber string) (*dto.AccountResponse, error)
	// ExtendPreview estimates where a purchase of packageID would leave the Active
	// Period. The estimate uses the same rule provisioning applies (ADR 0017).
	ExtendPreview(ctx context.Context, phoneNumber string, packageID uint) (*dto.ExtendPreviewResponse, error)
}

type service struct {
	transactions transactionservice.Service
	catalog      packageservice.Service
	expirations  provisioning.ExpirationReader
	clock        clock.Clock
	settings     Settings
	logger       *zap.Logger
}

// NewService builds the read composition; Fx injects every domain interface and
// the shared clock.
func NewService(
	transactions transactionservice.Service,
	catalog packageservice.Service,
	expirations provisioning.ExpirationReader,
	clock clock.Clock,
	settings Settings,
	logger *zap.Logger,
) Service {
	return &service{
		transactions: transactions,
		catalog:      catalog,
		expirations:  expirations,
		clock:        clock,
		settings:     settings,
		logger:       logger,
	}
}

func (s *service) Summary(ctx context.Context, phoneNumber string) (*dto.AccountResponse, error) {
	now := s.clock.Now()

	current, err := s.expirations.Expiration(ctx, phoneNumber)
	if err != nil {
		s.logger.Error("read Active Period for Account Status", zap.Error(err))
		return nil, fmt.Errorf("read Active Period: %w", err)
	}

	response := &dto.AccountResponse{
		PhoneNumber:  phoneNumber,
		ActivePeriod: activePeriod(current, now, s.settings.ExpiryWarning),
	}

	account, err := s.transactions.CurrentAccount(ctx, phoneNumber)
	if err != nil {
		return nil, err
	}
	if account == nil {
		// No provisioned purchase: PIN and Package stay null so the app can show
		// the empty state that points at the catalog.
		return response, nil
	}

	response.PIN = &account.PIN
	response.Package = s.packageSummary(ctx, account.PackageID)
	return response, nil
}

func (s *service) ExtendPreview(
	ctx context.Context,
	phoneNumber string,
	packageID uint,
) (*dto.ExtendPreviewResponse, error) {
	pkg, err := s.catalog.GetPackage(ctx, packageID)
	switch {
	case errors.Is(err, packageservice.ErrPackageNotFound):
		return nil, ErrPackageUnknown
	case err != nil:
		return nil, fmt.Errorf("read Package: %w", err)
	}

	now := s.clock.Now()
	current, err := s.expirations.Expiration(ctx, phoneNumber)
	if err != nil {
		s.logger.Error("read Active Period for the Extend preview", zap.Error(err))
		return nil, fmt.Errorf("read Active Period: %w", err)
	}

	duration := time.Duration(pkg.DurationMinutes) * time.Minute
	target := provisioning.Target(current, now, now, duration)

	response := &dto.ExtendPreviewResponse{
		PackageID:           pkg.ID,
		EstimatedExpiration: target.UTC().Format(time.RFC3339),
		Extends:             current.Found && current.ExpiresAt.After(now),
	}
	if current.Found {
		expiresAt := current.ExpiresAt.UTC().Format(time.RFC3339)
		response.CurrentExpiration = &expiresAt
	}
	return response, nil
}

// packageSummary reads the catalog row the current account bought. A Package
// removed from the catalog is not an error for the summary: the Active Period and
// the PIN are still the buyer's, so the Package block is simply absent.
func (s *service) packageSummary(ctx context.Context, packageID uint) *dto.PackageSummaryResponse {
	pkg, err := s.catalog.GetPackage(ctx, packageID)
	if err != nil {
		if !errors.Is(err, packageservice.ErrPackageNotFound) {
			s.logger.Error("read active Package for Account Status",
				zap.Uint("package_id", packageID), zap.Error(err))
		}
		return nil
	}
	return &dto.PackageSummaryResponse{ID: pkg.ID, Name: pkg.Name, RateLimit: pkg.RateLimit}
}

// activePeriod turns the FreeRADIUS expiration into the one state the app renders.
// The boundary is `<=`, the same comparison the Login Page uses, so an Active
// Period with exactly the warning window left is "expiring" on both surfaces.
func activePeriod(
	current provisioning.Expiration,
	now time.Time,
	warning time.Duration,
) dto.ActivePeriodResponse {
	if !current.Found || current.ExpiresAt.IsZero() {
		return dto.ActivePeriodResponse{Found: false, State: dto.StateNone}
	}

	expiresAt := current.ExpiresAt.UTC()
	state := dto.StateActive
	switch remaining := expiresAt.Sub(now); {
	case remaining <= 0:
		state = dto.StateExpired
	case remaining <= warning:
		state = dto.StateExpiring
	}

	formatted := expiresAt.Format(time.RFC3339)
	return dto.ActivePeriodResponse{ExpiresAt: &formatted, Found: true, State: state}
}
