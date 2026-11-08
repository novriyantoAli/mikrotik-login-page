package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
)

// revealActor is the signed-in support operator these tests reveal as.
func revealActor() audit.Actor {
	return audit.Actor{ID: 4, Email: "support@example.test", IP: "203.0.113.9"}
}

// seededProvisioned writes one provisioned Transaction whose PIN is sealed with the
// fixture's key, the way provisioning leaves it (ADR 0016).
func seededProvisioned(t *testing.T, f *fixture, plaintext string) *entity.Transaction {
	t.Helper()

	ciphertext, err := testPINCipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("seal the test PIN: %v", err)
	}

	row := entity.Transaction{
		ID:            41,
		Reference:     "TRX-REVEAL-0001",
		PhoneNumber:   testPhone,
		PackageID:     7,
		PackageName:   "Paket Mingguan",
		AmountIDR:     25000,
		Channel:       "BRIVA",
		State:         entity.StateProvisioned,
		PINCiphertext: ciphertext,
		PINKeyVersion: testPINCipher.ActiveVersion(),
		CreatedAt:     f.clock.now,
		ExpiresAt:     f.clock.now.Add(24 * time.Hour),
	}
	f.repo.rows = append(f.repo.rows, row)
	return &row
}

// TestRevealPIN_ReturnsThePINAndLeavesOneRowWithoutIt is #36's center: support gets
// the value, the trail gets one `pin.reveal` row naming the Transaction, and the row
// carries no credential (ADR 0027 §4).
func TestRevealPIN_ReturnsThePINAndLeavesOneRowWithoutIt(t *testing.T) {
	f := newFixture()
	row := seededProvisioned(t, f, "123456")

	value, err := f.service.RevealPIN(context.Background(), revealActor(), row.ID)
	if err != nil {
		t.Fatalf("reveal = %v", err)
	}
	if value != "123456" {
		t.Fatalf("revealed %q, want the sealed PIN", value)
	}

	if len(f.repo.revealEntries) != 1 {
		t.Fatalf("audit rows = %d, want exactly 1", len(f.repo.revealEntries))
	}
	entry := f.repo.revealEntries[0]
	if entry.Action != audit.ActionPINReveal {
		t.Fatalf("action = %q, want %q", entry.Action, audit.ActionPINReveal)
	}
	if entry.TargetType != audit.TargetTransaction || entry.TargetID != "41" {
		t.Fatalf("target = %q/%q, want transaction/41", entry.TargetType, entry.TargetID)
	}
	if entry.Actor != revealActor() {
		t.Fatalf("actor = %+v, want the signed-in admin", entry.Actor)
	}
	if entry.Payload["result"] != "ok" || entry.Payload["reference"] != row.Reference {
		t.Fatalf("payload = %v, want the outcome and the Transaction reference", entry.Payload)
	}

	// The trail is a document a reader filters on; it must not become a place a PIN
	// can be read back from (ADR 0027 §4).
	encoded, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal the audit entry: %v", err)
	}
	if strings.Contains(string(encoded), value) {
		t.Fatalf("the audit row carries the plaintext PIN: %s", encoded)
	}
}

// TestRevealPIN_AuditsAFailedDecryptOnceAndLeaksNothing covers "the attempt is the
// interesting fact": an unreadable key is worth seeing, answered generically, and
// recorded without the ciphertext or a candidate plaintext.
func TestRevealPIN_AuditsAFailedDecryptOnceAndLeaksNothing(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	f := newFixtureWithLogger(zap.New(core))
	row := seededProvisioned(t, f, "654321")
	// The row names a key version this deployment does not hold (ADR 0016).
	f.repo.rows[0].PINKeyVersion = 9

	value, err := f.service.RevealPIN(context.Background(), revealActor(), row.ID)
	if value != "" {
		t.Fatalf("revealed %q on a key fault, want nothing", value)
	}
	if !errors.Is(err, service.ErrPINUnavailable) {
		t.Fatalf("error = %v, want ErrPINUnavailable", err)
	}

	if len(f.repo.revealEntries) != 1 {
		t.Fatalf("audit rows = %d, want exactly 1", len(f.repo.revealEntries))
	}
	entry := f.repo.revealEntries[0]
	if entry.Payload["result"] != "decrypt_failed" {
		t.Fatalf("payload = %v, want the failure outcome", entry.Payload)
	}

	// Neither the log nor the error may carry the ciphertext or the plaintext.
	for _, logged := range logs.All() {
		rendered := logged.Message + fieldDump(logged)
		if strings.Contains(rendered, "654321") || strings.Contains(rendered, string(row.PINCiphertext)) {
			t.Fatalf("the failure leaked PIN material: %s", rendered)
		}
	}
	if strings.Contains(err.Error(), "654321") {
		t.Fatalf("the error leaks the plaintext: %v", err)
	}
}

// TestRevealPIN_RefusesWithoutAnActor keeps an ended session from leaving an
// anonymous trail row: an audited action with no actor is not representable
// (ADR 0027 §3).
func TestRevealPIN_RefusesWithoutAnActor(t *testing.T) {
	f := newFixture()
	row := seededProvisioned(t, f, "111111")

	_, err := f.service.RevealPIN(context.Background(), audit.Actor{}, row.ID)
	if !errors.Is(err, service.ErrActorRequired) {
		t.Fatalf("error = %v, want ErrActorRequired", err)
	}
	if len(f.repo.revealEntries) != 0 {
		t.Fatalf("audit rows = %d, want none without an actor", len(f.repo.revealEntries))
	}
}

// TestRevealPIN_AuditsAnUnknownTransaction: asking for the PIN of a Transaction that
// is not there is still a privileged attempt, so it is kept — and the answer does not
// tell the caller whether the id ever existed (ADR 0027 §4).
func TestRevealPIN_AuditsAnUnknownTransaction(t *testing.T) {
	f := newFixture()

	_, err := f.service.RevealPIN(context.Background(), revealActor(), 999)
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if len(f.repo.revealEntries) != 1 {
		t.Fatalf("audit rows = %d, want exactly 1", len(f.repo.revealEntries))
	}
	if f.repo.revealEntries[0].Payload["result"] != "not_found" {
		t.Fatalf("payload = %v, want the not-found outcome", f.repo.revealEntries[0].Payload)
	}
}

// TestRevealPIN_RefusesATransactionWithoutAPIN: a paid-but-unprovisioned row has no
// PIN yet, which is a conflict to retry later, not a key fault.
func TestRevealPIN_RefusesATransactionWithoutAPIN(t *testing.T) {
	f := newFixture()
	row := seededProvisioned(t, f, "222222")
	f.repo.rows[0].State = entity.StatePaid
	f.repo.rows[0].PINCiphertext = nil

	_, err := f.service.RevealPIN(context.Background(), revealActor(), row.ID)
	if !errors.Is(err, service.ErrPINNotReady) {
		t.Fatalf("error = %v, want ErrPINNotReady", err)
	}
	if len(f.repo.revealEntries) != 1 {
		t.Fatalf("audit rows = %d, want exactly 1", len(f.repo.revealEntries))
	}
	if f.repo.revealEntries[0].Payload["result"] != "not_ready" {
		t.Fatalf("payload = %v, want the not-ready outcome", f.repo.revealEntries[0].Payload)
	}
}

// TestRevealPIN_FailsClosedWhenTheTrailCannotBeWritten: the row is the price of
// reading a credential, so a trail that cannot be written must not hand the PIN out
// (ADR 0027 §2).
func TestRevealPIN_FailsClosedWhenTheTrailCannotBeWritten(t *testing.T) {
	f := newFixture()
	row := seededProvisioned(t, f, "333333")
	f.repo.revealErr = errors.New("audit table is gone")

	value, err := f.service.RevealPIN(context.Background(), revealActor(), row.ID)
	if err == nil {
		t.Fatal("expected the reveal to be refused when its audit row cannot be written")
	}
	if value != "" {
		t.Fatalf("revealed %q without a trail row", value)
	}
}

// TestRevealPIN_AuditsEveryRepeat: the row count is per attempt, not per Transaction,
// so a repeated reveal is visible as the repeated act it is (ADR 0027 §4).
func TestRevealPIN_AuditsEveryRepeat(t *testing.T) {
	f := newFixture()
	row := seededProvisioned(t, f, "444444")

	for attempt := 0; attempt < 3; attempt++ {
		if _, err := f.service.RevealPIN(context.Background(), revealActor(), row.ID); err != nil {
			t.Fatalf("reveal %d = %v", attempt+1, err)
		}
	}
	if len(f.repo.revealEntries) != 3 {
		t.Fatalf("audit rows = %d, want one per attempt", len(f.repo.revealEntries))
	}
}

// fieldDump renders an observed log line's fields for a substring assertion.
func fieldDump(entry observer.LoggedEntry) string {
	encoded, err := json.Marshal(entry.ContextMap())
	if err != nil {
		return ""
	}
	return string(encoded)
}
