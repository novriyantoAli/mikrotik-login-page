package service_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"sort"
	"testing"
	"time"

	"go.uber.org/zap"

	packagesdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	packagesentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	packageservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/pin"
)

const (
	testPhone     = "6281200000001"
	otherPhone    = "6281200000002"
	goodSignature = "good"
	badSignature  = "bad"
)

// --- fakes -----------------------------------------------------------------

type fakeRepository struct {
	rows []entity.Transaction
	next uint

	createErr       error
	pendingErr      error
	pendingSequence []*entity.Transaction
	pendingCalls    int
	transitionErr   error
	transitionCount int
	reconcilableErr error
	attemptErr      error
}

var _ repository.Repository = (*fakeRepository)(nil)

func (r *fakeRepository) Create(_ context.Context, row *entity.Transaction) error {
	if r.createErr != nil {
		return r.createErr
	}
	for _, existing := range r.rows {
		if existing.Reference == row.Reference {
			return errors.New("duplicate reference")
		}
	}
	r.next++
	row.ID = r.next
	if row.CreatedAt.IsZero() {
		row.CreatedAt = row.ExpiresAt
	}
	r.rows = append(r.rows, *row)
	return nil
}

func (r *fakeRepository) GetByID(_ context.Context, id uint) (*entity.Transaction, error) {
	for index := range r.rows {
		if r.rows[index].ID == id {
			copied := r.rows[index]
			return &copied, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *fakeRepository) GetByReference(_ context.Context, reference string) (*entity.Transaction, error) {
	for index := range r.rows {
		if r.rows[index].Reference == reference {
			copied := r.rows[index]
			return &copied, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *fakeRepository) Pending(
	_ context.Context,
	phoneNumber string,
	packageID uint,
	now time.Time,
) (*entity.Transaction, error) {
	if r.pendingErr != nil {
		return nil, r.pendingErr
	}
	if r.pendingSequence != nil {
		index := r.pendingCalls
		r.pendingCalls++
		if index < len(r.pendingSequence) {
			return r.pendingSequence[index], nil
		}
		return nil, nil
	}

	for index := range r.rows {
		row := r.rows[index]
		if row.PhoneNumber == phoneNumber && row.PackageID == packageID &&
			row.State == entity.StatePending && row.ExpiresAt.After(now) {
			copied := row
			return &copied, nil
		}
	}
	return nil, nil
}

func (r *fakeRepository) Transition(
	_ context.Context,
	id uint,
	from, to entity.State,
	at time.Time,
) (bool, error) {
	if r.transitionErr != nil {
		return false, r.transitionErr
	}
	for index := range r.rows {
		if r.rows[index].ID != id || r.rows[index].State != from {
			continue
		}
		r.rows[index].State = to
		r.rows[index].UpdatedAt = at
		if to == entity.StatePaid {
			paidAt := at
			r.rows[index].PaidAt = &paidAt
		}
		r.transitionCount++
		return true, nil
	}
	return false, nil
}

func (r *fakeRepository) ExpirePending(
	_ context.Context,
	phoneNumber string,
	packageID uint,
	now time.Time,
) ([]repository.Expired, error) {
	expired := make([]repository.Expired, 0, 1)
	for index := range r.rows {
		row := &r.rows[index]
		if row.PhoneNumber == phoneNumber && row.PackageID == packageID &&
			row.State == entity.StatePending && !row.ExpiresAt.After(now) {
			row.State = entity.StateExpired
			row.UpdatedAt = now
			expired = append(expired, expiredRow(row))
		}
	}
	return expired, nil
}

func (r *fakeRepository) ExpireOverdue(_ context.Context, now time.Time) ([]repository.Expired, error) {
	expired := make([]repository.Expired, 0, len(r.rows))
	for index := range r.rows {
		row := &r.rows[index]
		if row.State != entity.StatePending || row.ExpiresAt.After(now) {
			continue
		}
		row.State = entity.StateExpired
		row.UpdatedAt = now
		expired = append(expired, expiredRow(row))
	}
	return expired, nil
}

func expiredRow(row *entity.Transaction) repository.Expired {
	return repository.Expired{ID: row.ID, Reference: row.Reference, PhoneNumber: row.PhoneNumber}
}

// BumpProvisionAttempts mirrors the repository's counter: it increments and
// returns the new value, which is what the attempt log line reports (ADR 0018).
func (r *fakeRepository) BumpProvisionAttempts(_ context.Context, id uint, _ time.Time) (int, error) {
	if r.attemptErr != nil {
		return 0, r.attemptErr
	}
	for index := range r.rows {
		if r.rows[index].ID != id {
			continue
		}
		r.rows[index].ProvisionAttempts++
		return r.rows[index].ProvisionAttempts, nil
	}
	return 0, repository.ErrNotFound
}

func (r *fakeRepository) SetProvisioningPlan(
	_ context.Context,
	id uint,
	target time.Time,
	ciphertext []byte,
	keyVersion int,
	at time.Time,
) (bool, error) {
	for index := range r.rows {
		row := &r.rows[index]
		if row.ID != id || row.ProvisionTargetExpiration != nil || len(row.PINCiphertext) > 0 {
			continue
		}
		row.ProvisionTargetExpiration = &target
		row.PINCiphertext = ciphertext
		row.PINKeyVersion = keyVersion
		row.UpdatedAt = at
		return true, nil
	}
	return false, nil
}

func (r *fakeRepository) WithTx(
	ctx context.Context,
	fn func(context.Context, database.Tx) error,
) error {
	// Snapshot the rows so a failing callback leaves the fake in the state a real
	// rollback would leave it in, which is what the atomicity test asserts.
	snapshot := append([]entity.Transaction(nil), r.rows...)
	if err := fn(ctx, database.Tx{}); err != nil {
		r.rows = snapshot
		return err
	}
	return nil
}

func (r *fakeRepository) MarkProvisioned(
	_ context.Context,
	_ database.Tx,
	id uint,
	at time.Time,
) (bool, error) {
	for index := range r.rows {
		if r.rows[index].ID != id || r.rows[index].State != entity.StatePaid {
			continue
		}
		r.rows[index].State = entity.StateProvisioned
		r.rows[index].UpdatedAt = at
		return true, nil
	}
	return false, nil
}

func (r *fakeRepository) PaidIDs(_ context.Context, limit int) ([]uint, error) {
	var ids []uint
	for index := range r.rows {
		if r.rows[index].State != entity.StatePaid {
			continue
		}
		ids = append(ids, r.rows[index].ID)
		if len(ids) == limit {
			break
		}
	}
	return ids, nil
}

// ReconcilableIDs mirrors the repository's selector: pending rows whose deadline
// is still ahead, soonest first (ADR 0018).
func (r *fakeRepository) ReconcilableIDs(_ context.Context, now time.Time, limit int) ([]uint, error) {
	if r.reconcilableErr != nil {
		return nil, r.reconcilableErr
	}

	type candidate struct {
		id        uint
		expiresAt time.Time
	}
	matched := make([]candidate, 0, len(r.rows))
	for index := range r.rows {
		row := r.rows[index]
		if row.State != entity.StatePending || !row.ExpiresAt.After(now) {
			continue
		}
		matched = append(matched, candidate{id: row.ID, expiresAt: row.ExpiresAt})
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].expiresAt.Before(matched[j].expiresAt) })

	ids := make([]uint, 0, len(matched))
	for _, row := range matched {
		ids = append(ids, row.id)
		if len(ids) == limit {
			break
		}
	}
	return ids, nil
}

func (r *fakeRepository) ListByPhone(
	_ context.Context,
	phoneNumber string,
	params repository.ListParams,
) ([]entity.Transaction, int64, error) {
	matched := make([]entity.Transaction, 0, len(r.rows))
	for index := len(r.rows) - 1; index >= 0; index-- {
		if r.rows[index].PhoneNumber == phoneNumber {
			matched = append(matched, r.rows[index])
		}
	}

	total := int64(len(matched))
	start := (params.Page - 1) * params.PageSize
	if start >= len(matched) {
		return []entity.Transaction{}, total, nil
	}
	end := start + params.PageSize
	if end > len(matched) {
		end = len(matched)
	}
	return matched[start:end], total, nil
}

func (r *fakeRepository) LatestProvisioned(
	_ context.Context,
	phoneNumber string,
) (*entity.Transaction, error) {
	var latest *entity.Transaction
	for index := range r.rows {
		row := r.rows[index]
		if row.PhoneNumber != phoneNumber || row.State != entity.StateProvisioned {
			continue
		}
		if latest == nil || row.ID > latest.ID {
			copied := row
			latest = &copied
		}
	}
	return latest, nil
}

type fakeCatalog struct {
	packages map[uint]packagesentity.Package
}

func (c *fakeCatalog) ListPackages(context.Context) ([]packagesdto.PackageResponse, error) {
	return nil, nil
}

func (c *fakeCatalog) GetPackage(_ context.Context, id uint) (*packagesentity.Package, error) {
	pkg, ok := c.packages[id]
	if !ok {
		return nil, packageservice.ErrPackageNotFound
	}
	return &pkg, nil
}

func (c *fakeCatalog) Seed(context.Context) (int64, error) { return 0, nil }

type fakeGateway struct {
	channels    []payment.Channel
	channelsErr error
	createErr   error
	callbacks   map[string]*payment.Callback
	createCalls int
	// statuses and statusErr drive the reconciliation sweep; an unlisted
	// reference answers "unpaid" like the vendor does for a checkout nobody paid.
	statuses      map[string]payment.PaymentStatus
	statusErr     error
	statusQueries []string
}

func (g *fakeGateway) CreateTransaction(
	_ context.Context,
	req payment.CreateTransactionRequest,
) (*payment.CreateTransactionResponse, error) {
	if g.createErr != nil {
		return nil, g.createErr
	}
	g.createCalls++
	return &payment.CreateTransactionResponse{
		Reference:   req.Reference,
		ChannelName: req.Channel,
		PayCode:     "8808812345678901",
		Instructions: []payment.Instruction{
			{Title: "Cara Pembayaran", Steps: []string{"Buka aplikasi bank"}},
		},
		ExpiresAt: req.ExpiresAt,
	}, nil
}

func (g *fakeGateway) GetStatus(_ context.Context, reference string) (*payment.PaymentStatus, error) {
	g.statusQueries = append(g.statusQueries, reference)
	if g.statusErr != nil {
		return nil, g.statusErr
	}
	if status, ok := g.statuses[reference]; ok {
		return &status, nil
	}
	return &payment.PaymentStatus{Reference: reference, Status: payment.StatusUnpaid}, nil
}

func (g *fakeGateway) VerifyCallback(raw []byte, signature string) (*payment.Callback, error) {
	if signature != goodSignature {
		return nil, payment.ErrSignatureMismatch
	}
	callback, ok := g.callbacks[string(raw)]
	if !ok {
		return nil, errors.New("unknown callback payload")
	}
	return callback, nil
}

func (g *fakeGateway) Channels(context.Context) ([]payment.Channel, error) {
	if g.channelsErr != nil {
		return nil, g.channelsErr
	}
	return g.channels, nil
}

// testPINCipher is a fixed development key: the tests care about the round-trip,
// not about the secret (ADR 0016).
var testPINCipher = mustCipher()

func mustCipher() *pin.Cipher {
	cipher, err := pin.NewCipher(pin.Settings{
		Keys:          "1=" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x2a}, pin.KeyBytes)),
		ActiveVersion: 1,
	})
	if err != nil {
		panic(err)
	}
	return cipher
}

type recordingWaker struct{ tasks []string }

func (w *recordingWaker) Enqueue(_ context.Context, taskType string) error {
	w.tasks = append(w.tasks, taskType)
	return nil
}

type fixture struct {
	repo        *fakeRepository
	catalog     *fakeCatalog
	gateway     *fakeGateway
	waker       *recordingWaker
	clock       *fakeClock
	writer      *fakeRadiusWriter
	reader      *fakeRadiusReader
	outbox      *fakeSMSOutbox
	service     service.Service
	provisioner service.Provisioner
	reconciler  service.Reconciler
	logger      *zap.Logger
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Advance(duration time.Duration) { c.now = c.now.Add(duration) }

func newFixture() *fixture {
	return newFixtureWithLogger(zap.NewNop())
}

// newFixtureWithLogger builds the same fixture with a caller-supplied logger, so a
// test can assert the structured events a transition emits (ADR 0018).
func newFixtureWithLogger(logger *zap.Logger) *fixture {
	f := &fixture{
		logger: logger,
		repo:   &fakeRepository{},
		catalog: &fakeCatalog{packages: map[uint]packagesentity.Package{
			7: {ID: 7, Name: "Paket Mingguan", PriceIDR: 25000, DurationMinutes: 10080, RateLimit: "10M/10M"},
		}},
		gateway: &fakeGateway{
			channels: []payment.Channel{
				{
					Code: "BRIVA", Name: "BRIVA", Group: "Virtual Account",
					FeeCustomer: payment.ChannelFee{FlatIDR: 2500},
				},
			},
			callbacks: make(map[string]*payment.Callback),
			statuses:  make(map[string]payment.PaymentStatus),
		},
		waker: &recordingWaker{},
		clock: &fakeClock{now: time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)},
	}
	f.writer = &fakeRadiusWriter{}
	f.reader = &fakeRadiusReader{}
	f.outbox = &fakeSMSOutbox{}
	f.provisioner = service.NewProvisioner(
		f.repo,
		f.catalog,
		f.writer,
		f.reader,
		f.outbox,
		testPINCipher,
		f.clock,
		logger,
	)
	f.service = service.NewService(
		f.repo,
		f.catalog,
		f.gateway,
		testPINCipher,
		f.waker,
		f.clock,
		service.Settings{
			PaymentTTL:         time.Hour,
			LoginPageAllowlist: []string{"https://hotspot.local/login"},
		},
		logger,
	)
	f.reconciler = service.NewReconciler(
		f.repo,
		f.gateway,
		f.waker,
		f.clock,
		service.ReconciliationSettings{SweepInterval: time.Minute, Batch: 50},
		logger,
	)
	return f
}

func (f *fixture) checkout(t *testing.T, request dto.CreateTransactionRequest) *dto.TransactionResponse {
	t.Helper()

	transaction, err := f.service.Create(context.Background(), testPhone, &request)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return transaction
}

func validRequest() dto.CreateTransactionRequest {
	return dto.CreateTransactionRequest{PackageID: 7, Channel: "BRIVA"}
}

// --- checkout --------------------------------------------------------------

func TestCreate_SecondRequestReturnsTheSamePendingTransaction(t *testing.T) {
	t.Parallel()

	f := newFixture()
	first := f.checkout(t, validRequest())
	second := f.checkout(t, validRequest())

	if first.ID != second.ID || first.Reference != second.Reference {
		t.Fatalf("second checkout returned a different Transaction: %+v vs %+v", first, second)
	}
	if f.gateway.createCalls != 1 {
		t.Fatalf("gateway was asked %d times, want 1: a repeat tap must not open a second bill",
			f.gateway.createCalls)
	}
	if len(f.repo.rows) != 1 {
		t.Fatalf("stored %d Transactions, want 1", len(f.repo.rows))
	}
}

func TestCreate_SnapshotsTheTypedPackagePrice(t *testing.T) {
	t.Parallel()

	f := newFixture()
	transaction := f.checkout(t, validRequest())

	if transaction.AmountIDR != 25000 {
		t.Fatalf("amount = %d, want the typed Package price 25000", transaction.AmountIDR)
	}
	if transaction.PackageName != "Paket Mingguan" {
		t.Fatalf("package name = %q", transaction.PackageName)
	}
	if transaction.State != string(entity.StatePending) {
		t.Fatalf("state = %q, want pending", transaction.State)
	}
	if len(transaction.Instructions) != 1 {
		t.Fatalf("instructions lost: %+v", transaction.Instructions)
	}
}

func TestCreate_UnknownPackageIsRejectedBeforeCallingTheGateway(t *testing.T) {
	t.Parallel()

	f := newFixture()
	_, err := f.service.Create(context.Background(), testPhone,
		&dto.CreateTransactionRequest{PackageID: 999, Channel: "BRIVA"})

	if !errors.Is(err, service.ErrPackageUnknown) {
		t.Fatalf("err = %v, want ErrPackageUnknown", err)
	}
	if f.gateway.createCalls != 0 {
		t.Fatalf("gateway was called %d times for an unknown Package", f.gateway.createCalls)
	}
}

func TestCreate_UnknownChannelIsRejectedBeforeCallingTheGateway(t *testing.T) {
	t.Parallel()

	f := newFixture()
	_, err := f.service.Create(context.Background(), testPhone,
		&dto.CreateTransactionRequest{PackageID: 7, Channel: "KREDIT"})

	if !errors.Is(err, service.ErrChannelUnknown) {
		t.Fatalf("err = %v, want ErrChannelUnknown", err)
	}
	if f.gateway.createCalls != 0 {
		t.Fatalf("gateway was called %d times for an unknown channel", f.gateway.createCalls)
	}
}

func TestCreate_GatewayFailureIsADependencyError(t *testing.T) {
	t.Parallel()

	f := newFixture()
	f.gateway.createErr = errors.New("dial timeout")

	_, err := f.service.Create(context.Background(), testPhone, &dto.CreateTransactionRequest{
		PackageID: 7, Channel: "BRIVA",
	})
	if !errors.Is(err, service.ErrGatewayUnavailable) {
		t.Fatalf("err = %v, want ErrGatewayUnavailable", err)
	}
	if len(f.repo.rows) != 0 {
		t.Fatal("a failed gateway call must not leave a Transaction behind")
	}
}

func TestCreate_ReturnTargetIsOnlyKeptWhenTheAllowlistAcceptsIt(t *testing.T) {
	t.Parallel()

	f := newFixture()
	allowed := validRequest()
	allowed.ReturnURL = "https://hotspot.local/login?mac=AA:BB"
	if got := f.checkout(t, allowed).ReturnURL; got != allowed.ReturnURL {
		t.Fatalf("return_url = %q, want %q", got, allowed.ReturnURL)
	}

	f = newFixture()
	refused := validRequest()
	refused.ReturnURL = "https://evil.example/login"
	if got := f.checkout(t, refused).ReturnURL; got != "" {
		t.Fatalf("return_url = %q, want it dropped by the allowlist", got)
	}
}

func TestCreate_LosingThePendingRaceReturnsTheExistingTransaction(t *testing.T) {
	t.Parallel()

	f := newFixture()
	existing := entity.Transaction{
		ID: 42, Reference: "TRX-EXISTING", PhoneNumber: testPhone, PackageID: 7,
		State: entity.StatePending, AmountIDR: 25000,
		ExpiresAt: f.clock.now.Add(30 * time.Minute),
	}
	f.repo.rows = append(f.repo.rows, existing)
	f.repo.createErr = errors.New("duplicate key value violates unique constraint")
	// First Pending (before the gateway call) misses, the second one — after the
	// insert lost the race — finds the winner.
	f.repo.pendingSequence = []*entity.Transaction{nil, &existing}

	transaction := f.checkout(t, validRequest())

	if transaction.ID != existing.ID {
		t.Fatalf("id = %d, want the existing pending Transaction %d", transaction.ID, existing.ID)
	}
}

// --- reading ---------------------------------------------------------------

func TestGet_AnotherPhoneNumberIsNotFound(t *testing.T) {
	t.Parallel()

	f := newFixture()
	transaction := f.checkout(t, validRequest())

	if _, err := f.service.Get(context.Background(), otherPhone, transaction.ID); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound (never forbidden: it would confirm the Transaction)", err)
	}
	if _, err := f.service.Get(context.Background(), testPhone, transaction.ID); err != nil {
		t.Fatalf("own Transaction: %v", err)
	}
}

// --- callbacks -------------------------------------------------------------

func (f *fixture) callback(t *testing.T, reference string, status payment.Status, amount int64) ([]byte, string) {
	t.Helper()

	raw := []byte("callback:" + reference + ":" + string(status))
	f.gateway.callbacks[string(raw)] = &payment.Callback{
		Reference: reference,
		Status:    status,
		Amount:    amount,
	}
	return raw, goodSignature
}

func TestHandleCallback_MarksPendingPaidOnce(t *testing.T) {
	t.Parallel()

	f := newFixture()
	transaction := f.checkout(t, validRequest())
	raw, signature := f.callback(t, transaction.Reference, payment.StatusPaid, 25000)

	if err := f.service.HandleCallback(context.Background(), raw, signature); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if err := f.service.HandleCallback(context.Background(), raw, signature); err != nil {
		t.Fatalf("repeat HandleCallback: %v", err)
	}

	if f.repo.rows[0].State != entity.StatePaid {
		t.Fatalf("state = %q, want paid", f.repo.rows[0].State)
	}
	if f.repo.transitionCount != 1 {
		t.Fatalf("transitions = %d, want exactly 1: a repeated callback must not transition twice",
			f.repo.transitionCount)
	}
	// The worker is woken once, not twice: the repeated callback changes nothing and
	// provisioning has one durable work item per paid Transaction (ADR 0016).
	if len(f.waker.tasks) != 1 || f.waker.tasks[0] != service.TaskProvision {
		t.Fatalf("wake-ups = %v, want exactly one %q", f.waker.tasks, service.TaskProvision)
	}
}

func TestHandleCallback_UnsignedCallbackIsRejected(t *testing.T) {
	t.Parallel()

	f := newFixture()
	transaction := f.checkout(t, validRequest())
	raw, _ := f.callback(t, transaction.Reference, payment.StatusPaid, 25000)

	err := f.service.HandleCallback(context.Background(), raw, badSignature)
	if !errors.Is(err, payment.ErrSignatureMismatch) {
		t.Fatalf("err = %v, want payment.ErrSignatureMismatch", err)
	}
	if f.repo.rows[0].State != entity.StatePending {
		t.Fatalf("state = %q, want the Transaction untouched", f.repo.rows[0].State)
	}
}

func TestHandleCallback_AmountMismatchIsRefused(t *testing.T) {
	t.Parallel()

	f := newFixture()
	transaction := f.checkout(t, validRequest())
	raw, signature := f.callback(t, transaction.Reference, payment.StatusPaid, 1000)

	if err := f.service.HandleCallback(context.Background(), raw, signature); !errors.Is(err, service.ErrAmountMismatch) {
		t.Fatalf("err = %v, want ErrAmountMismatch", err)
	}
	if f.repo.rows[0].State != entity.StatePending {
		t.Fatalf("state = %q, want the Transaction untouched", f.repo.rows[0].State)
	}
}

func TestHandleCallback_PaidCannotBecomeExpired(t *testing.T) {
	t.Parallel()

	f := newFixture()
	transaction := f.checkout(t, validRequest())
	paidRaw, signature := f.callback(t, transaction.Reference, payment.StatusPaid, 25000)
	if err := f.service.HandleCallback(context.Background(), paidRaw, signature); err != nil {
		t.Fatalf("settle: %v", err)
	}

	expiredRaw, signature := f.callback(t, transaction.Reference, payment.StatusExpired, 25000)
	if err := f.service.HandleCallback(context.Background(), expiredRaw, signature); err != nil {
		t.Fatalf("late expiry callback: %v", err)
	}

	if f.repo.rows[0].State != entity.StatePaid {
		t.Fatalf("state = %q, want paid: paid → expired is a forbidden transition", f.repo.rows[0].State)
	}
}

func TestHandleCallback_FailedClosesAPendingTransaction(t *testing.T) {
	t.Parallel()

	f := newFixture()
	transaction := f.checkout(t, validRequest())
	raw, signature := f.callback(t, transaction.Reference, payment.StatusFailed, 25000)

	if err := f.service.HandleCallback(context.Background(), raw, signature); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if f.repo.rows[0].State != entity.StateFailed {
		t.Fatalf("state = %q, want failed", f.repo.rows[0].State)
	}
}

func TestHandleCallback_UnknownReferenceIsAcknowledgedWithoutChangingAnything(t *testing.T) {
	t.Parallel()

	f := newFixture()
	raw, signature := f.callback(t, "TRX-UNKNOWN", payment.StatusPaid, 25000)

	if err := f.service.HandleCallback(context.Background(), raw, signature); err != nil {
		t.Fatalf("an unknown reference must be acknowledged, not retried forever: %v", err)
	}
	if len(f.repo.rows) != 0 {
		t.Fatal("a callback for an unknown reference must not create a Transaction")
	}
}

// --- expiry ----------------------------------------------------------------

func TestCreate_AfterTheDeadlineOpensANewTransactionEvenBeforeTheSweep(t *testing.T) {
	t.Parallel()

	f := newFixture()
	overdue := f.checkout(t, validRequest())

	// The worker has not swept yet; checkout itself must treat the deadline as
	// final, otherwise the one-pending index would block a fresh purchase.
	f.clock.Advance(2 * time.Hour)
	fresh := f.checkout(t, validRequest())

	if fresh.ID == overdue.ID {
		t.Fatal("an overdue pending Transaction was returned instead of being closed")
	}
	for _, row := range f.repo.rows {
		if row.ID == overdue.ID && row.State != entity.StateExpired {
			t.Fatalf("overdue Transaction state = %q, want expired", row.State)
		}
	}
}

func TestExpireDue_ClosesOnlyOverduePendingTransactions(t *testing.T) {
	t.Parallel()

	f := newFixture()
	overdue := f.checkout(t, validRequest())

	f.clock.Advance(2 * time.Hour)
	expired, err := service.NewExpiry(f.repo, f.clock, zap.NewNop()).ExpireDue(context.Background())
	if err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired %d Transactions, want 1", expired)
	}
	if f.repo.rows[0].State != entity.StateExpired {
		t.Fatalf("state = %q, want expired", f.repo.rows[0].State)
	}

	// The deadline is a one-way door: a checkout after expiry opens a new
	// Transaction instead of reviving the old one.
	fresh := f.checkout(t, validRequest())
	if fresh.ID == overdue.ID {
		t.Fatal("checkout after expiry reused the expired Transaction")
	}
}

// ProvisionCounts mirrors the repository's two aggregate numbers, so the operator
// counter can be tested without a database (ADR 0018).
func (r *fakeRepository) ProvisionCounts(context.Context) (repository.ProvisionCounts, error) {
	counts := repository.ProvisionCounts{}
	for index := range r.rows {
		if r.rows[index].State != entity.StatePaid {
			continue
		}
		counts.PaidAwaiting++
		counts.Attempts += int64(r.rows[index].ProvisionAttempts)
	}
	return counts, nil
}
