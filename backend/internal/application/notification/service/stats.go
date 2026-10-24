package service

import (
	"context"
	"fmt"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
)

// OutboxCounts is the outbox's state at one instant, in the numbers an operator
// acts on (ADR 0018). OldestPendingAgeSeconds measures how long the longest-waiting
// buyer has been waiting, from the row's creation, not when it was last tried.
type OutboxCounts struct {
	Pending                 int64
	Sending                 int64
	Sent                    int64
	Failed                  int64
	OldestPendingAgeSeconds int64
}

// OutboxCounter reads the outbox for the worker's operator surface. It is a
// separate port rather than another method on Outbox, so domains that only send
// SMS keep depending on a two-method interface.
type OutboxCounter interface {
	CountOutbox(ctx context.Context) (OutboxCounts, error)
}

type outboxCounter struct {
	repo  repository.Repository
	clock clock.Clock
}

// NewOutboxCounter builds the counter; the clock is injected so the age is
// testable without waiting.
func NewOutboxCounter(repo repository.Repository, clk clock.Clock) OutboxCounter {
	return &outboxCounter{repo: repo, clock: clk}
}

func (c *outboxCounter) CountOutbox(ctx context.Context) (OutboxCounts, error) {
	counts, err := c.repo.Counts(ctx, c.clock.Now())
	if err != nil {
		return OutboxCounts{}, fmt.Errorf("count sms_outbox: %w", err)
	}
	return OutboxCounts{
		Pending:                 counts.Pending,
		Sending:                 counts.Sending,
		Sent:                    counts.Sent,
		Failed:                  counts.Failed,
		OldestPendingAgeSeconds: counts.OldestPendingAgeSeconds,
	}, nil
}
