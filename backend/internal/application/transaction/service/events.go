package service

import (
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
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

// changeSubject is the identity of the Transaction a transition happened to. The
// three fields always travel together, so they travel as one value instead of
// being repeated at every call site (ADR 0018).
type changeSubject struct {
	ID          uint
	Reference   string
	PhoneNumber string
}

// subjectOf is the subject of a row the caller is already holding.
func subjectOf(row *entity.Transaction) changeSubject {
	return changeSubject{ID: row.ID, Reference: row.Reference, PhoneNumber: row.PhoneNumber}
}

// subjectOfClosed is the same identity for a row the repository already reduced to
// the three fields a log line needs. The two bulk expiry paths hand back
// repository.Expired, so converting it lives here instead of at both call sites.
func subjectOfClosed(row repository.Expired) changeSubject {
	return changeSubject{ID: row.ID, Reference: row.Reference, PhoneNumber: row.PhoneNumber}
}

// logStateChange is the one place a Transaction transition reaches the log, so
// every reason writes the same keys; two spellings of the same event would make
// the log unqueryable (ADR 0018). extra carries context that belongs to one reason
// only (the settled amount, say) without changing the shared shape.
func logStateChange(
	logger *zap.Logger,
	subject changeSubject,
	from, to entity.State,
	reason string,
	extra ...zap.Field,
) {
	fields := make([]zap.Field, 0, 7+len(extra))
	fields = append(fields,
		zap.String("event", EventStateChanged),
		zap.Uint("transaction_id", subject.ID),
		zap.String("from", string(from)),
		zap.String("to", string(to)),
		zap.String("reference", subject.Reference),
		// The Phone Number is the account identity, so it is masked here even
		// though the PIN never reaches a log line at all (ADR 0018).
		zap.String("phone", phone.Mask(subject.PhoneNumber)),
		zap.String("reason", reason),
	)
	fields = append(fields, extra...)

	logger.Info("transaction state changed", fields...)
}

// logDeadlinesClosed is the one place a deadline closing reaches the log. Both bulk
// expiry paths (the worker's sweep and a checkout superseding its own overdue row)
// make the same transition and differ only in reason, so they share one loop.
func logDeadlinesClosed(logger *zap.Logger, rows []repository.Expired, reason string) {
	for _, row := range rows {
		logStateChange(logger, subjectOfClosed(row), entity.StatePending, entity.StateExpired, reason)
	}
}

// provisioningAttempt is one logged provisioning attempt. Attempt is the running
// count for this Transaction — 0 when the counter itself was unavailable, because
// a failed observation must not fail the attempt (ADR 0018). PhoneNumber is carried
// masked so an operator can pair the attempt with the buyer without joining on the
// state-change line.
type provisioningAttempt struct {
	TransactionID uint
	PhoneNumber   string
	Attempt       int
	Outcome       string
}

func (a provisioningAttempt) log(logger *zap.Logger, cause error) {
	fields := []zap.Field{
		zap.String("event", EventProvisioningAttempt),
		zap.Uint("transaction_id", a.TransactionID),
		zap.String("phone", phone.Mask(a.PhoneNumber)),
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
