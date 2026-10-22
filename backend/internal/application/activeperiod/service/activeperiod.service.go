// Package service reads a Phone Number's Active Period end from FreeRADIUS and
// formats it for the Login Page status page. The vendor schema stays behind the
// provisioning.ExpirationReader port (ADR 0014).
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/activeperiod/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/phone"
)

// ErrInvalidPhoneNumber is returned when the query value cannot be a Phone
// Number. It is a client error, not a missing account.
var ErrInvalidPhoneNumber = errors.New("invalid phone number")

// Service answers the public Active Period read.
type Service interface {
	// GetActivePeriod normalises the Phone Number and returns the end of its
	// Active Period, or a response with a nil Expiry when FreeRADIUS holds no
	// expiration for it.
	GetActivePeriod(ctx context.Context, phoneNumber string) (*dto.ActivePeriodResponse, error)
}

type service struct {
	reader provisioning.ExpirationReader
	logger *zap.Logger
}

// NewService builds the service; Fx injects the FreeRADIUS read port.
func NewService(reader provisioning.ExpirationReader, logger *zap.Logger) Service {
	return &service{reader: reader, logger: logger}
}

func (s *service) GetActivePeriod(ctx context.Context, phoneNumber string) (*dto.ActivePeriodResponse, error) {
	normalized, err := phone.Normalize(phoneNumber)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidPhoneNumber, err)
	}

	expiration, err := s.reader.Expiration(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("read active period for a phone number: %w", err)
	}
	if !expiration.Found {
		return &dto.ActivePeriodResponse{}, nil
	}

	expiry := expiration.ExpiresAt.UTC().Format(time.RFC3339)
	return &dto.ActivePeriodResponse{Expiry: &expiry}, nil
}
