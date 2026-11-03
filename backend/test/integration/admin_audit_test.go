package integration

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	adminrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
	adminservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
	packagesrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/repository"
	transactionrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// panelActor is the signed-in owner these tests audit as.
func panelActor(t *testing.T, harness *testutil.APIHarness) audit.Actor {
	t.Helper()

	owner := readOwner(t, harness)
	return audit.Actor{ID: owner.ID, Email: owner.Email, IP: "203.0.113.9"}
}

func readAuditRows(t *testing.T, harness *testutil.APIHarness) []entity.AuditEntry {
	t.Helper()

	var rows []entity.AuditEntry
	if err := harness.Postgres.Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("read audit rows: %v", err)
	}
	return rows
}

// TestAuditRowCommitsWithTheCallersTransaction is ADR 0027 §2 through the port the
// catalog declares: the row the adapter writes is only visible because the caller's
// transaction committed it.
func TestAuditRowCommitsWithTheCallersTransaction(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	// The catalog's own port is what the adapter must satisfy; nothing in this test
	// names the admin domain's concrete type except the constructor.
	var port packagesrepository.AuditRecorder = adminrepository.NewAuditRecorder(zap.NewNop())

	err := harness.Postgres.Transaction(func(tx *gorm.DB) error {
		return port.Record(context.Background(), database.NewTx(tx), audit.Entry{
			Actor:      panelActor(t, harness),
			Action:     audit.ActionPackageUpdate,
			TargetType: audit.TargetPackage,
			TargetID:   "3",
			Payload:    map[string]any{"price_idr": map[string]any{"before": 5000, "after": 7500}},
		})
	})
	if err != nil {
		t.Fatalf("record inside a committed transaction: %v", err)
	}

	rows := readAuditRows(t, harness)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want exactly 1", len(rows))
	}

	owner := readOwner(t, harness)
	row := rows[0]
	if row.Action != audit.ActionPackageUpdate {
		t.Fatalf("action = %q, want %q", row.Action, audit.ActionPackageUpdate)
	}
	if row.AdminID != owner.ID || row.AdminEmail != owner.Email {
		t.Fatalf("actor = %d/%q, want %d/%q", row.AdminID, row.AdminEmail, owner.ID, owner.Email)
	}
	if row.TargetType != string(audit.TargetPackage) || row.TargetID != "3" {
		t.Fatalf("target = %q/%q, want package/3", row.TargetType, row.TargetID)
	}
	if row.IP != "203.0.113.9" {
		t.Fatalf("ip = %q, want the actor's address", row.IP)
	}
}

// TestAuditRowRollsBackWithTheCallersTransaction is the other direction of the same
// promise: a change that fails must not leave a witness claiming it happened.
func TestAuditRowRollsBackWithTheCallersTransaction(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	var port transactionrepository.AuditRecorder = adminrepository.NewAuditRecorder(zap.NewNop())
	rolledBack := errors.New("the changed row refused to save")

	err := harness.Postgres.Transaction(func(tx *gorm.DB) error {
		if recordErr := port.Record(context.Background(), database.NewTx(tx), audit.Entry{
			Actor:      panelActor(t, harness),
			Action:     audit.ActionPINReveal,
			TargetType: audit.TargetTransaction,
			TargetID:   "42",
		}); recordErr != nil {
			return recordErr
		}
		// The change this row witnesses never happened.
		return rolledBack
	})
	if !errors.Is(err, rolledBack) {
		t.Fatalf("error = %v, want the caller's rollback", err)
	}

	if rows := readAuditRows(t, harness); len(rows) != 0 {
		t.Fatalf("audit rows = %d after a rollback, want none", len(rows))
	}
}

// TestAuditRecordRefusesToWriteOutsideATransaction makes "one row, one transaction"
// an invariant rather than a habit: without the caller's handle there is no row.
func TestAuditRecordRefusesToWriteOutsideATransaction(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	recorder := adminrepository.NewAuditRecorder(zap.NewNop())

	err := recorder.Record(context.Background(), database.Tx{}, audit.Entry{
		Actor:  panelActor(t, harness),
		Action: audit.ActionAdminDeactivate,
	})
	if !errors.Is(err, adminrepository.ErrAuditWithoutTransaction) {
		t.Fatalf("error = %v, want ErrAuditWithoutTransaction", err)
	}
	if rows := readAuditRows(t, harness); len(rows) != 0 {
		t.Fatalf("audit rows = %d, want none", len(rows))
	}
}

// TestAuditRecordRefusesThePoolInsteadOfATransaction closes the other door to the
// same failure: a handle that is not a transaction would commit the row on its own,
// so it is refused exactly like a missing handle.
func TestAuditRecordRefusesThePoolInsteadOfATransaction(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	recorder := adminrepository.NewAuditRecorder(zap.NewNop())

	// The pool itself, wrapped as if it were a caller's transaction.
	err := recorder.Record(context.Background(), database.NewTx(harness.Postgres), audit.Entry{
		Actor:  panelActor(t, harness),
		Action: audit.ActionAdminCreate,
	})
	if !errors.Is(err, adminrepository.ErrAuditWithoutTransaction) {
		t.Fatalf("error = %v, want ErrAuditWithoutTransaction", err)
	}
	if rows := readAuditRows(t, harness); len(rows) != 0 {
		t.Fatalf("audit rows = %d, want none", len(rows))
	}
}

// TestCreateAdminLeavesExactlyOneAuditRow is #30's acceptance criterion against a
// real database: one write, one row, and a taken address leaves no second row.
func TestCreateAdminLeavesExactlyOneAuditRow(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()

	repo := adminrepository.NewRepository(
		harness.Postgres,
		adminrepository.NewAuditRecorder(zap.NewNop()),
		zap.NewNop(),
	)
	panel := adminservice.NewService(repo, adminservice.BootstrapSettings{}, zap.NewNop())
	actor := panelActor(t, harness)

	created, err := panel.CreateAdmin(ctx, actor, &dto.CreateAdminRequest{
		Email:    "Support@Example.TEST",
		Role:     "support",
		Password: "kata-sandi-yang-cukup-panjang",
	})
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	rows := readAuditRows(t, harness)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want exactly 1", len(rows))
	}

	row := rows[0]
	if row.Action != audit.ActionAdminCreate {
		t.Fatalf("action = %q, want %q", row.Action, audit.ActionAdminCreate)
	}
	if row.AdminID != actor.ID || row.AdminEmail != actor.Email {
		t.Fatalf("actor = %d/%q, want %d/%q", row.AdminID, row.AdminEmail, actor.ID, actor.Email)
	}
	if row.TargetType != string(audit.TargetAdmin) || row.TargetID != strconv.FormatUint(uint64(created.ID), 10) {
		t.Fatalf("target = %q/%q, want admin/%d", row.TargetType, row.TargetID, created.ID)
	}
	if row.Payload["email"] != "support@example.test" || row.Payload["role"] != string(entity.RoleSupport) {
		t.Fatalf("payload = %v, want the fields the owner chose", row.Payload)
	}

	// A second attempt on the same address is a conflict, and a conflict is not an
	// action: the trail must not grow (ADR 0027 §Consequences).
	if _, err := panel.CreateAdmin(ctx, actor, &dto.CreateAdminRequest{
		Email:    "support@example.test",
		Role:     "support",
		Password: "kata-sandi-yang-cukup-panjang",
	}); !errors.Is(err, adminservice.ErrEmailTaken) {
		t.Fatalf("second create error = %v, want ErrEmailTaken", err)
	}

	if rows := readAuditRows(t, harness); len(rows) != 1 {
		t.Fatalf("audit rows after a refused create = %d, want 1", len(rows))
	}

	var admins int64
	if err := harness.Postgres.Model(&entity.Admin{}).Count(&admins).Error; err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if admins != 2 {
		t.Fatalf("admins = %d, want the seeded owner plus the created support", admins)
	}
}

// TestCreateAdminRollsBackWhenTheTrailCannotBeWritten proves the transaction covers
// both statements, not just the happy path: if the audit row cannot be stored, the
// admin it describes must not exist either. The trail table is renamed away for the
// duration of the call, because a schema this throwaway is the cheapest way to break
// exactly one statement.
func TestCreateAdminRollsBackWhenTheTrailCannotBeWritten(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()

	repo := adminrepository.NewRepository(
		harness.Postgres,
		adminrepository.NewAuditRecorder(zap.NewNop()),
		zap.NewNop(),
	)
	panel := adminservice.NewService(repo, adminservice.BootstrapSettings{}, zap.NewNop())
	actor := panelActor(t, harness)

	if err := harness.Postgres.Exec(`ALTER TABLE admin_audit_log RENAME TO admin_audit_log_gone`).Error; err != nil {
		t.Fatalf("rename the trail table: %v", err)
	}

	if _, err := panel.CreateAdmin(ctx, actor, &dto.CreateAdminRequest{
		Email:    "unwitnessed@example.test",
		Role:     "support",
		Password: "kata-sandi-yang-cukup-panjang",
	}); err == nil {
		t.Fatal("a write whose audit row cannot be stored must fail")
	}

	var created int64
	if err := harness.Postgres.Model(&entity.Admin{}).
		Where("email = ?", "unwitnessed@example.test").
		Count(&created).Error; err != nil {
		t.Fatalf("count the refused admin: %v", err)
	}
	if created != 0 {
		t.Fatalf("a rolled-back create left %d admin rows behind", created)
	}
}
