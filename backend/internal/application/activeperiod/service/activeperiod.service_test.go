package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/activeperiod/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/phone"
)

type fakeReader struct {
	expiration provisioning.Expiration
	err        error
	asked      []string
}

func (r *fakeReader) Expiration(_ context.Context, phoneNumber string) (provisioning.Expiration, error) {
	r.asked = append(r.asked, phoneNumber)
	return r.expiration, r.err
}

func TestGetActivePeriod_AnswersCanonicalISO8601InUTC(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	reader := &fakeReader{expiration: provisioning.Expiration{ExpiresAt: expiresAt, Found: true}}
	svc := service.NewService(reader)

	response, err := svc.GetActivePeriod(context.Background(), "0812-3456-789")
	if err != nil {
		t.Fatalf("GetActivePeriod: %v", err)
	}
	if response.Expiry == nil {
		t.Fatal("expiry is nil, want a timestamp")
	}
	if *response.Expiry != "2026-01-10T12:00:00Z" {
		t.Fatalf("expiry = %q, want %q", *response.Expiry, "2026-01-10T12:00:00Z")
	}
	if len(reader.asked) != 1 || reader.asked[0] != "628123456789" {
		t.Fatalf("reader was asked for %v, want the canonical number", reader.asked)
	}
}

func TestGetActivePeriod_NonUTCTimeIsNormalised(t *testing.T) {
	t.Parallel()

	jakarta := time.FixedZone("WIB", 7*60*60)
	expiresAt := time.Date(2026, 1, 10, 19, 0, 0, 0, jakarta)
	reader := &fakeReader{expiration: provisioning.Expiration{ExpiresAt: expiresAt, Found: true}}

	response, err := service.NewService(reader).
		GetActivePeriod(context.Background(), "+62 812 3456 789")
	if err != nil {
		t.Fatalf("GetActivePeriod: %v", err)
	}
	if *response.Expiry != "2026-01-10T12:00:00Z" {
		t.Fatalf("expiry = %q, want the same instant in UTC", *response.Expiry)
	}
}

func TestGetActivePeriod_UnknownNumberIsNullExpiryNotAnError(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{expiration: provisioning.Expiration{Found: false}}
	response, err := service.NewService(reader).
		GetActivePeriod(context.Background(), "628123456789")
	if err != nil {
		t.Fatalf("GetActivePeriod: %v", err)
	}
	if response.Expiry != nil {
		t.Fatalf("expiry = %q, want null", *response.Expiry)
	}
}

func TestGetActivePeriod_RejectsAMalformedPhoneNumberWithoutReading(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{}
	_, err := service.NewService(reader).GetActivePeriod(context.Background(), "not-a-number")
	if !errors.Is(err, phone.ErrInvalid) {
		t.Fatalf("error = %v, want phone.ErrInvalid", err)
	}
	if len(reader.asked) != 0 {
		t.Fatalf("reader was called %d times for an invalid number", len(reader.asked))
	}
}

func TestGetActivePeriod_FreeRADIUSFailureIsReturned(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{err: errors.New("mysql is down")}
	_, err := service.NewService(reader).
		GetActivePeriod(context.Background(), "628123456789")
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, phone.ErrInvalid) {
		t.Fatal("a dependency failure must not look like a client error")
	}
}
