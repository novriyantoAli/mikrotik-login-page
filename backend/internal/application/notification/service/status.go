package service

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/repository"
)

// DeliveryStatus is what became of the last SMS this system tried to send to one
// Phone Number. It is a read-only value for another domain's operator surface, not
// a row: it deliberately carries no message body, because a pending row still holds
// the PIN in clear text (ADR 0016).
type DeliveryStatus struct {
	// State is one of pending, sending, sent or failed.
	State string
	// UpdatedAt is when that state was last written.
	UpdatedAt time.Time
}

// StatusReader is the outbox's read port for the panel's Transaction list. It is a
// separate port rather than another method on Outbox, so a domain that only sends
// SMS keeps depending on a two-method interface — the same shape OutboxCounter has
// (ADR 0018).
type StatusReader interface {
	// LatestStatuses answers, for each Phone Number, the newest outbox row's state.
	// A number that was never messaged is absent from the map; the caller must
	// treat "absent" as "no message yet", not as a failure.
	LatestStatuses(ctx context.Context, phoneNumbers []string) (map[string]DeliveryStatus, error)
}

type statusReader struct {
	repo   repository.Repository
	logger *zap.Logger
}

// NewStatusReader builds the read-only outbox surface; Fx injects the repository.
func NewStatusReader(repo repository.Repository, logger *zap.Logger) StatusReader {
	return &statusReader{repo: repo, logger: logger}
}

func (r *statusReader) LatestStatuses(
	ctx context.Context,
	phoneNumbers []string,
) (map[string]DeliveryStatus, error) {
	unique := uniqueNumbers(phoneNumbers)
	if len(unique) == 0 {
		return map[string]DeliveryStatus{}, nil
	}

	rows, err := r.repo.LatestStatuses(ctx, unique)
	if err != nil {
		return nil, fmt.Errorf("read latest sms statuses: %w", err)
	}

	statuses := make(map[string]DeliveryStatus, len(rows))
	for _, row := range rows {
		statuses[row.PhoneNumber] = DeliveryStatus{State: row.State, UpdatedAt: row.UpdatedAt}
	}
	return statuses, nil
}

// uniqueNumbers drops the empty values and the duplicates a page of Transactions
// can contain: the same buyer may appear several times, and the query only needs
// each number once.
func uniqueNumbers(phoneNumbers []string) []string {
	seen := make(map[string]struct{}, len(phoneNumbers))
	unique := make([]string, 0, len(phoneNumbers))
	for _, phoneNumber := range phoneNumbers {
		if phoneNumber == "" {
			continue
		}
		if _, exists := seen[phoneNumber]; exists {
			continue
		}
		seen[phoneNumber] = struct{}{}
		unique = append(unique, phoneNumber)
	}
	return unique
}
