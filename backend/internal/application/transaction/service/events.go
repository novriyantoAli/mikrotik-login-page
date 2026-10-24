package service

import (
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/phone"
)

// Event names and reasons are stable log values, not prose: an operator queries
// them by name, so renaming one breaks a dashboard nobody re-runs the tests for
// (ADR 0018).
const (
	// EventStateChanged is emitted once per Transaction transition.
	EventStateChanged = "transaction.state_changed"
	// EventProvisioningAttempt is emitted once per provisioning attempt.
	EventProvisioningAttempt = "provisioning.attempt"

	// ReasonGatewayCallback is a settlement that arrived as a signed callback.
	ReasonGatewayCallback = "gateway_callback"
	// ReasonReconciliation is a settlement the reconciliation sweep asked about.
	ReasonReconciliation = "reconciliation"
	// ReasonExpirySweep is the deadline passing, closed by the worker.
	ReasonExpirySweep = "expiry_sweep"
	// ReasonCheckoutSuperseded is an overdue checkout closed by the same buyer's
	// next one, before the sweep reached it.
	ReasonCheckoutSuperseded = "checkout_superseded"
	// ReasonProvisioning is the worker turning a paid Transaction into an account.
	ReasonProvisioning = "provisioning"
)

// Provisioning attempt outcomes. There is no `failed`: a paid Transaction is never
// abandoned, so every unsuccessful attempt is one the next sweep retries
// (ADR 0016, ADR 0018).
const (
	OutcomeOK    = "ok"
	OutcomeRetry = "retry"
)

// stateChange is one logged transition. It exists as a type so every caller
// writes the same keys; two spellings of the same event would make the log
// unqueryable (ADR 0018).
type stateChange struct {
	TransactionID uint
	Reference     string
	PhoneNumber   string
	From          entity.State
	To            entity.State
	Reason        string
}

// log writes the transition. extra carries context that belongs to one reason
// only (the settled amount, say) without changing the shared shape.
func (c stateChange) log(logger *zap.Logger, extra ...zap.Field) {
	fields := make([]zap.Field, 0, 7+len(extra))
	fields = append(fields,
		zap.String("event", EventStateChanged),
		zap.Uint("transaction_id", c.TransactionID),
		zap.String("from", string(c.From)),
		zap.String("to", string(c.To)),
		zap.String("reference", c.Reference),
		// The Phone Number is the account identity, so it is masked here even
		// though the PIN never reaches a log line at all (ADR 0018).
		zap.String("phone", phone.Mask(c.PhoneNumber)),
		zap.String("reason", c.Reason),
	)
	fields = append(fields, extra...)

	logger.Info("transaction state changed", fields...)
}

// provisioningAttempt is one logged provisioning attempt. Attempt is the running
// count for this Transaction — 0 when the counter itself was unavailable, because
// a failed observation must not fail the attempt (ADR 0018).
type provisioningAttempt struct {
	TransactionID uint
	Attempt       int
	Outcome       string
}

func (a provisioningAttempt) log(logger *zap.Logger, cause error) {
	fields := []zap.Field{
		zap.String("event", EventProvisioningAttempt),
		zap.Uint("transaction_id", a.TransactionID),
		zap.Int("attempt", a.Attempt),
		zap.String("outcome", a.Outcome),
	}
	if cause != nil {
		// The cause is a dependency error (MySQL down, a broken plan), never a PIN
		// or a Phone Number: those are logged masked or not at all.
		fields = append(fields, zap.Error(cause))
	}

	logger.Info("provisioning attempt", fields...)
}
