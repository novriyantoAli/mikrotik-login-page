package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	notificationservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
	packagesentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	packageservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
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
	repo    repository.Repository
	catalog packageservice.Service
	writer  provisioning.Writer
	reader  provisioning.ExpirationReader
	outbox  notificationservice.Outbox
	cipher  *pin.Cipher
	clock   clock.Clock
	logger  *zap.Logger
}

// NewProvisioner builds the provisioner; Fx injects every port, so the MySQL
// writer and the SMS outbox stay replaceable in tests.
func NewProvisioner(
	repo repository.Repository,
	catalog packageservice.Service,
	writer provisioning.Writer,
	reader provisioning.ExpirationReader,
	outbox notificationservice.Outbox,
	cipher *pin.Cipher,
	clock clock.Clock,
	logger *zap.Logger,
) Provisioner {
	return &provisioner{
		repo:    repo,
		catalog: catalog,
		writer:  writer,
		reader:  reader,
		outbox:  outbox,
		cipher:  cipher,
		clock:   clock,
		logger:  logger,
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

// provision does the work of one attempt: read the Package, persist the plan once,
// write the account, then record the result and its SMS together.
func (p *provisioner) provision(ctx context.Context, row *entity.Transaction) error {
	pkg, err := p.catalog.GetPackage(ctx, row.PackageID)
	if err != nil {
		return fmt.Errorf("read Package %d: %w", row.PackageID, err)
	}

	row, err = p.ensurePlan(ctx, row, pkg)
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
		RateLimit:   pkg.RateLimit,
	}); err != nil {
		return fmt.Errorf("apply hotspot account: %w", err)
	}

	return p.complete(ctx, row, pinValue)
}

// ensurePlan stores the target Active Period end and the encrypted PIN exactly
// once. Losing the guard means another attempt already decided them, so the
// stored row — never a fresh calculation — is what this attempt applies
// (ADR 0016).
func (p *provisioner) ensurePlan(
	ctx context.Context,
	row *entity.Transaction,
	pkg *packagesentity.Package,
) (*entity.Transaction, error) {
	if row.ProvisionTargetExpiration != nil && len(row.PINCiphertext) > 0 {
		return row, nil
	}

	target, err := p.targetExpiration(ctx, row, pkg)
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
	pkg *packagesentity.Package,
) (time.Time, error) {
	duration := time.Duration(pkg.DurationMinutes) * time.Minute
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
