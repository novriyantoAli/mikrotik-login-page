package service

import (
	"context"
	"fmt"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
)

// ProvisioningCounts is the provisioning backlog at one instant (ADR 0018).
// Attempts is a gauge, not a counter: it sums the attempts recorded on rows still
// waiting, so it is zero when nothing is stuck and rises while a backlog retries.
type ProvisioningCounts struct {
	PaidAwaiting int64
	Attempts     int64
}

// ProvisioningCounter reads the provisioning backlog for the worker's operator
// surface. It is separate from Provisioner because reading what is owed and doing
// the work are different jobs with different failure modes.
type ProvisioningCounter interface {
	CountProvisioning(ctx context.Context) (ProvisioningCounts, error)
}

type provisioningCounter struct {
	repo repository.Repository
}

// NewProvisioningCounter builds the counter over the Transaction repository.
func NewProvisioningCounter(repo repository.Repository) ProvisioningCounter {
	return &provisioningCounter{repo: repo}
}

func (c *provisioningCounter) CountProvisioning(ctx context.Context) (ProvisioningCounts, error) {
	counts, err := c.repo.ProvisionCounts(ctx)
	if err != nil {
		return ProvisioningCounts{}, fmt.Errorf("count paid transactions: %w", err)
	}
	return ProvisioningCounts{PaidAwaiting: counts.PaidAwaiting, Attempts: counts.Attempts}, nil
}
