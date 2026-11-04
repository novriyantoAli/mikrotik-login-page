package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	notificationservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/pin"
)

const (
	// TaskProvision is the payload-free wake-up the provisioning worker listens
	// for. It names no Transaction because the durable work item is "every paid
	// Transaction not provisioned yet" (ADR 0016).
	TaskProvision = "transaction:provision"
	// provisionBatch bounds one sweep, so a backlog is chipped away instead of
	// holding the worker for minutes.
	provisionBatch = 20
)

// ErrSnapshotMissing means a paid Transaction carries none of the catalog terms
// checkout is supposed to freeze on it. Refusing is the point: reading the
// catalog would grant today's terms for a purchase that was sold at older ones
// (ADR 0026 §5).
var ErrSnapshotMissing = errors.New("transaction carries no catalog terms")

// Provisioner turns a paid Transaction into a working hotspot account: it writes
// the PIN, the Active Period end and the rate limit to FreeRADIUS, then records
// the SMS in the outbox and the state change in one Postgres transaction
// (PRD point 22, ADR 0016). Every method is safe to call repeatedly.
type Provisioner interface {
	// Provision finishes one paid Transaction. A Transaction in any other state is
	// left alone; `provisioned` is already done.
	Provision(ctx context.Context, id uint) error
	// ProvisionDue provisions the paid Transactions still owed, newest batch
	// first, and reports how many it completed.
	ProvisionDue(ctx context.Context) (int, error)
}

type provisioner struct {
	repo   repository.Repository
	writer provisioning.Writer
	reader provisioning.ExpirationReader
	outbox notificationservice.Outbox
	cipher *pin.Cipher
	clock  clock.Clock
	logger *zap.Logger
}

// NewProvisioner builds the provisioner; Fx injects every port, so the MySQL
// writer and the SMS outbox stay replaceable in tests.
func NewProvisioner(
	repo repository.Repository,
	writer provisioning.Writer,
	reader provisioning.ExpirationReader,
	outbox notificationservice.Outbox,
	cipher *pin.Cipher,
	clock clock.Clock,
	logger *zap.Logger,
) Provisioner {
	return &provisioner{
		repo:   repo,
		writer: writer,
		reader: reader,
		outbox: outbox,
		cipher: cipher,
		clock:  clock,
		logger: logger,
	}
}

func (p *provisioner) Provision(ctx context.Context, id uint) error {
	row, err := p.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return err
	}
	if row.State != entity.StatePaid {
		// `pending` has not been paid, `failed`/`expired` never will be, and
		// `provisioned` is already the goal (PRD point 15).
		return nil
	}

	attempt := p.beginAttempt(ctx, row.ID)
	logged := provisioningAttempt{
		TransactionID: row.ID,
		PhoneNumber:   row.PhoneNumber,
		Attempt:       attempt,
	}
	if err := p.provision(ctx, row); err != nil {
		logged.Outcome = OutcomeRetry
		logged.log(p.logger, err)
		return err
	}
	logged.Outcome = OutcomeOK
	logged.log(p.logger, nil)
	return nil
}

// beginAttempt counts this attempt before any work, so a stuck Transaction shows
// how often the worker tried. A counter that cannot be written is logged and
// ignored: an observation that breaks the observed is worse than a number that is
// missing (ADR 0018).
func (p *provisioner) beginAttempt(ctx context.Context, id uint) int {
	attempt, err := p.repo.BumpProvisionAttempts(ctx, id, p.clock.Now())
	if err != nil {
		p.logger.Warn("provisioning attempt counter unavailable",
			zap.Uint("transaction_id", id), zap.Error(err))
		return 0
	}
	return attempt
}

// provision does the work of one attempt: read the terms the buyer paid for,
// persist the plan once, write the account, then record the result and its SMS
// together.
func (p *provisioner) provision(ctx context.Context, row *entity.Transaction) error {
	terms, err := soldTermsOf(row)
	if err != nil {
		var missing *missingTermsError
		if errors.As(err, &missing) {
			p.logMissingSnapshot(row, *missing)
		}
		return err
	}

	row, err = p.ensurePlan(ctx, row, terms.duration)
	if err != nil {
		return err
	}

	if row.ProvisionTargetExpiration == nil || len(row.PINCiphertext) == 0 {
		// Only reachable if another writer left a half plan; refusing is safer than
		// guessing an Active Period end.
		return errors.New("provisioning plan is incomplete")
	}

	pinValue, err := p.cipher.Decrypt(row.PINCiphertext, row.PINKeyVersion)
	if err != nil {
		// Logged once, by the sweep that reports which Transaction failed. The PIN is
		// never part of the log line, only the failure.
		return fmt.Errorf("decrypt stored PIN: %w", err)
	}

	if err := p.writer.Apply(ctx, provisioning.Account{
		PhoneNumber: row.PhoneNumber,
		PIN:         pinValue,
		Expiration:  row.ProvisionTargetExpiration.UTC(),
		RateLimit:   terms.rateLimit,
	}); err != nil {
		return fmt.Errorf("apply hotspot account: %w", err)
	}

	return p.complete(ctx, row, pinValue)
}

// soldTerms are the catalog terms frozen on the Transaction at checkout: the
// Active Period length and the Mikrotik-Rate-Limit the buyer was sold. The name
// keeps it apart from catalogTerms in backfill.go, which are the values the
// catalog holds today.
type soldTerms struct {
	duration  time.Duration
	rateLimit string
}

// soldTermsOf reads the snapshot and nothing else. A missing field is a bug, not
// a deploy boundary: checkout always writes both, and `migrate` backfills the
// rows that predate them, so reading the catalog here would hand the buyer
// today's price for yesterday's purchase (ADR 0026 §5).
func soldTermsOf(row *entity.Transaction) (soldTerms, error) {
	if row.DurationMinutes <= 0 || row.RateLimit == "" {
		return soldTerms{}, &missingTermsError{
			transactionID:   row.ID,
			durationMissing: row.DurationMinutes <= 0,
			rateMissing:     row.RateLimit == "",
		}
	}
	return soldTerms{
		duration:  time.Duration(row.DurationMinutes) * time.Minute,
		rateLimit: row.RateLimit,
	}, nil
}

// missingTermsError carries which fields refused the attempt, so the refusal and
// the log line that explains it are decided by one predicate instead of two that
// can drift.
type missingTermsError struct {
	transactionID   uint
	durationMissing bool
	rateMissing     bool
}

func (e *missingTermsError) Error() string {
	return fmt.Sprintf("%s: transaction %d (duration_missing=%t, rate_limit_missing=%t)",
		ErrSnapshotMissing, e.transactionID, e.durationMissing, e.rateMissing)
}

// Unwrap makes errors.Is(err, ErrSnapshotMissing) true for callers and tests.
func (e *missingTermsError) Unwrap() error { return ErrSnapshotMissing }

// logMissingSnapshot names the fields a refused Transaction is missing. It is the
// replacement for the old fallback's log line: the attempt counter already shows
// that something is stuck, and this event says why.
func (p *provisioner) logMissingSnapshot(row *entity.Transaction, missing missingTermsError) {
	p.logger.Warn("provisioning refused: Transaction has no catalog terms",
		zap.String("event", EventSnapshotMissing),
		zap.Uint("transaction_id", row.ID),
		zap.Uint("package_id", row.PackageID),
		zap.Bool("duration_missing", missing.durationMissing),
		zap.Bool("rate_limit_missing", missing.rateMissing),
	)
}

// ensurePlan stores the target Active Period end and the encrypted PIN exactly
// once. Losing the guard means another attempt already decided them, so the
// stored row — never a fresh calculation — is what this attempt applies
// (ADR 0016).
func (p *provisioner) ensurePlan(
	ctx context.Context,
	row *entity.Transaction,
	duration time.Duration,
) (*entity.Transaction, error) {
	if row.ProvisionTargetExpiration != nil && len(row.PINCiphertext) > 0 {
		return row, nil
	}

	target, err := p.targetExpiration(ctx, row, duration)
	if err != nil {
		return nil, err
	}

	plaintext, err := pin.Generate()
	if err != nil {
		return nil, err
	}
	ciphertext, err := p.cipher.Encrypt(plaintext)
	if err != nil {
		return nil, fmt.Errorf("encrypt PIN: %w", err)
	}

	_, err = p.repo.SetProvisioningPlan(
		ctx, row.ID, target, ciphertext, p.cipher.ActiveVersion(), p.clock.Now(),
	)
	if err != nil {
		return nil, err
	}
	return p.repo.GetByID(ctx, row.ID)
}

// targetExpiration applies PRD point 21 through the one shared rule (ADR 0017):
// an Active Period still running is extended, an absent or finished one starts at
// the settlement time.
func (p *provisioner) targetExpiration(
	ctx context.Context,
	row *entity.Transaction,
	duration time.Duration,
) (time.Time, error) {
	now := p.clock.Now()

	current, err := p.reader.Expiration(ctx, row.PhoneNumber)
	if err != nil {
		return time.Time{}, fmt.Errorf("read current Active Period: %w", err)
	}

	start := now
	if row.PaidAt != nil {
		start = row.PaidAt.UTC()
	}
	return provisioning.Target(current, now, start, duration), nil
}

// complete records the state change and the PIN SMS in one Postgres transaction,
// so a provisioned Transaction always has its message queued (ADR 0010).
func (p *provisioner) complete(ctx context.Context, row *entity.Transaction, pinValue string) error {
	at := p.clock.Now()
	message := notificationservice.Message{PhoneNumber: row.PhoneNumber, Text: smsPINText(pinValue)}

	recorded := false
	err := p.repo.WithTx(ctx, func(ctx context.Context, tx database.Tx) error {
		changed, err := p.repo.MarkProvisioned(ctx, tx, row.ID, at)
		if err != nil || !changed {
			return err
		}
		if err := p.outbox.EnqueueTx(ctx, tx, message); err != nil {
			return err
		}
		recorded = true
		return nil
	})
	if err != nil {
		return fmt.Errorf("record provisioning: %w", err)
	}
	if !recorded {
		p.logger.Info("provisioning already recorded by another attempt",
			zap.Uint("transaction_id", row.ID))
		return nil
	}

	logStateChange(p.logger, subjectOf(row), entity.StatePaid, entity.StateProvisioned,
		ReasonProvisioning)
	return nil
}

func (p *provisioner) ProvisionDue(ctx context.Context) (int, error) {
	ids, err := p.repo.PaidIDs(ctx, provisionBatch)
	if err != nil {
		return 0, err
	}

	provisioned := 0
	for _, id := range ids {
		if err := p.Provision(ctx, id); err != nil {
			// One failure must not stop the sweep: the row stays paid and the next
			// sweep retries it once MySQL is back. The attempt is already logged
			// with its transaction id and count, so logging it again here would
			// only duplicate the same fact (ADR 0018).
			continue
		}
		provisioned++
	}
	return provisioned, nil
}

// smsPINText is the one place a PIN leaves the backend in clear text, because the
// buyer needs it to log in (ADR 0004).
func smsPINText(pinValue string) string {
	return fmt.Sprintf(
		"PIN Anda %s. Masuk ke hotspot dengan nomor ini dan PIN tersebut. Jangan bagikan PIN kepada siapa pun.",
		pinValue,
	)
}
