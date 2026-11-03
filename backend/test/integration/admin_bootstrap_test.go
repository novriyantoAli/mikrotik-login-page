package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/app"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	adminrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
	adminservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
	packagesentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// TestAdminSchemaAndOwnerSeed is the acceptance criterion for #29 end to end:
// `migrate` creates the three admin tables plus packages.is_active/version, seeds
// one owner from the environment, and running it again inserts nothing and
// rewrites nothing.
func TestAdminSchemaAndOwnerSeed(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()

	for _, table := range []string{"admins", "admin_sessions", "admin_audit_log"} {
		if !harness.Postgres.Migrator().HasTable(table) {
			t.Fatalf("migrate did not create table %q", table)
		}
	}

	assertPackagesColumnsAllowRetirement(t, harness)
	assertOwnerSeed(t, harness, testutil.AdminBootstrapEnv()["ADMIN_EMAIL"])

	var before int64
	if err := harness.Postgres.Model(&entity.Admin{}).Count(&before).Error; err != nil {
		t.Fatalf("count admins: %v", err)
	}
	seeded := readOwner(t, harness)

	// A redeploy must be a no-op: same row count, same password hash, and the
	// bootstrap service's own answer is "nothing to insert".
	harness.Migrate(t)
	inserted, err := harness.AdminBootstrap.BootstrapOwner(ctx)
	if err != nil {
		t.Fatalf("re-run the owner seed: %v", err)
	}
	if inserted != 0 {
		t.Fatalf("re-running the seed inserted %d admins, want 0", inserted)
	}

	var after int64
	if err := harness.Postgres.Model(&entity.Admin{}).Count(&after).Error; err != nil {
		t.Fatalf("count admins after re-run: %v", err)
	}
	if after != before {
		t.Fatalf("admins after re-run = %d, want %d", after, before)
	}
	if again := readOwner(t, harness); again.PasswordHash != seeded.PasswordHash {
		t.Fatal("re-running migrate rewrote the existing password hash")
	}

	// The environment is ignored once an admin exists: a different pair, an
	// incomplete one and an invalid one are all not even errors. Each case needs a
	// service carrying its own settings, which is why they are not the harness's.
	for name, settings := range map[string]adminservice.BootstrapSettings{
		"different pair": {Email: "other@example.test", Password: "kata-sandi-lain-yang-panjang"},
		"empty pair":     {},
		"invalid email":  {Email: "not-an-email", Password: "kata-sandi-lain-yang-panjang"},
		"weak password":  {Email: "owner@example.test", Password: "pendek"},
	} {
		t.Run(name, func(t *testing.T) {
			bootstrap := adminservice.NewService(
				adminrepository.NewRepository(harness.Postgres, adminrepository.NewAuditRecorder(zap.NewNop()), zap.NewNop()),
				settings,
				zap.NewNop(),
			)
			inserted, err := bootstrap.BootstrapOwner(ctx)
			if err != nil {
				t.Fatalf("BootstrapOwner with an existing admin: %v", err)
			}
			if inserted != 0 {
				t.Fatalf("inserted = %d, want 0", inserted)
			}
			if preserved := readOwner(t, harness); preserved.PasswordHash != seeded.PasswordHash {
				t.Fatal("bootstrap rewrote the existing password hash")
			}
		})
	}
}

// TestMigrateFailsLoudlyWithoutBootstrapCredentials runs the real migration
// command against a brand new schema: an empty admins table and no configured
// owner must stop the deploy with a message that names both variables, instead of
// leaving a panel nobody can enter (ADR 0024 §6).
func TestMigrateFailsLoudlyWithoutBootstrapCredentials(t *testing.T) {
	deps := testutil.RequireDependencies(t)
	schema := testutil.NewPostgresSchema(t, deps.PostgresDSN)
	testutil.ApplyPostgresEnv(t, deps.PostgresDSN, schema)
	testutil.ApplyPINEnv(t)
	t.Setenv("ADMIN_EMAIL", "")
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("LOGGER_LEVEL", "warn")

	err := app.RunOnce(app.Migrate())
	if err == nil {
		t.Fatal("migrate finished with an empty admins table and no bootstrap credentials")
	}
	for _, variable := range []string{"ADMIN_EMAIL", "ADMIN_PASSWORD"} {
		if !strings.Contains(err.Error(), variable) {
			t.Fatalf("error %q does not name %s", err, variable)
		}
	}

	db := testutil.OpenPostgres(t, deps.PostgresDSN, schema)
	var count int64
	if err := db.Model(&entity.Admin{}).Count(&count).Error; err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if count != 0 {
		t.Fatalf("a refused migrate left %d admin rows behind", count)
	}

	// The same command with the pair configured finishes and is idempotent.
	testutil.ApplyAdminBootstrapEnv(t)
	if err := app.RunOnce(app.Migrate()); err != nil {
		t.Fatalf("migrate with bootstrap credentials: %v", err)
	}
	if err := app.RunOnce(app.Migrate()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var seeded entity.Admin
	if err := db.First(&seeded).Error; err != nil {
		t.Fatalf("read seeded admin: %v", err)
	}
	if err := db.Model(&entity.Admin{}).Count(&count).Error; err != nil {
		t.Fatalf("count admins after seeding: %v", err)
	}
	if count != 1 {
		t.Fatalf("migrate produced %d admins, want exactly 1", count)
	}
}

// TestAdminSchemaEnforcesItsConstraints proves the three rules ADR 0024 relies on
// live in the database, not only in a future service: one admin per email, one
// session per token hash, and no fourth role.
func TestAdminSchemaEnforcesItsConstraints(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	owner := readOwner(t, harness)

	duplicate := owner
	duplicate.ID = 0
	duplicate.PasswordHash = owner.PasswordHash
	if err := harness.Postgres.Create(&duplicate).Error; err == nil {
		t.Fatal("a second admin with the same email was accepted")
	}

	uppercase := owner
	uppercase.ID = 0
	uppercase.Email = strings.ToUpper(owner.Email)
	if err := harness.Postgres.Create(&uppercase).Error; err == nil {
		t.Fatal("an email that is not canonical lowercase was accepted")
	}

	unknownRole := owner
	unknownRole.ID = 0
	unknownRole.Email = "superuser@example.test"
	unknownRole.Role = entity.Role("superuser")
	if err := harness.Postgres.Create(&unknownRole).Error; err == nil {
		t.Fatal("a role outside owner/support/auditor was accepted")
	}

	session := entity.Session{
		TokenHash: "hash-that-must-stay-unique",
		AdminID:   owner.ID,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := harness.Postgres.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	second := session
	second.ID = 0
	if err := harness.Postgres.Create(&second).Error; err == nil {
		t.Fatal("a second session with the same token hash was accepted")
	}
}

// TestAuditLogStoresAndReadsItsPayload proves the trail's JSONB column survives a
// round trip through the real driver: the before/after an auditor reads back must
// be the document that was written, not an encoded string (ADR 0027 §3).
func TestAuditLogStoresAndReadsItsPayload(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	owner := readOwner(t, harness)

	entry := entity.AuditEntry{
		AdminID:    owner.ID,
		AdminEmail: owner.Email,
		Action:     audit.ActionPackageUpdate,
		TargetType: "package",
		TargetID:   "7",
		Payload: entity.Payload{
			"before": map[string]any{"price_idr": 5000.0},
			"after":  map[string]any{"price_idr": 6000.0},
		},
		IP: "127.0.0.1",
	}
	if err := harness.Postgres.Create(&entry).Error; err != nil {
		t.Fatalf("insert audit row: %v", err)
	}

	var stored entity.AuditEntry
	if err := harness.Postgres.First(&stored, entry.ID).Error; err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	if stored.Action != audit.ActionPackageUpdate {
		t.Fatalf("action = %q, want %q", stored.Action, audit.ActionPackageUpdate)
	}
	if stored.AdminEmail != owner.Email {
		t.Fatalf("admin email = %q, want the copied %q", stored.AdminEmail, owner.Email)
	}
	after, ok := stored.Payload["after"].(map[string]any)
	if !ok {
		t.Fatalf("payload.after = %T, want a decoded object: %v", stored.Payload["after"], stored.Payload)
	}
	if after["price_idr"] != 6000.0 {
		t.Fatalf("payload.after.price_idr = %v, want 6000", after["price_idr"])
	}
}

// assertPackagesColumnsAllowRetirement pins ADR 0025's two new columns: a Package
// is on sale unless retired, and every Package starts at version 1 so a write can
// present it.
func assertPackagesColumnsAllowRetirement(t *testing.T, harness *testutil.APIHarness) {
	t.Helper()

	defaults := map[string]string{}
	var columns []struct {
		ColumnName    string `gorm:"column:column_name"`
		ColumnDefault string `gorm:"column:column_default"`
	}
	if err := harness.Postgres.Raw(
		`SELECT column_name, column_default FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = 'packages'`,
	).Scan(&columns).Error; err != nil {
		t.Fatalf("read packages columns: %v", err)
	}
	for _, column := range columns {
		defaults[column.ColumnName] = column.ColumnDefault
	}
	if !strings.Contains(defaults["is_active"], "true") {
		t.Fatalf("packages.is_active default = %q, want true", defaults["is_active"])
	}
	if defaults["version"] != "1" {
		t.Fatalf("packages.version default = %q, want 1", defaults["version"])
	}

	var catalog []packagesentity.Package
	if err := harness.Postgres.Order("id ASC").Find(&catalog).Error; err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	if len(catalog) == 0 {
		t.Fatal("the seeded catalog is empty")
	}
	for _, pkg := range catalog {
		if !pkg.IsActive {
			t.Fatalf("seeded Package %q is not active", pkg.Code)
		}
		if pkg.Version != 1 {
			t.Fatalf("seeded Package %q has version %d, want 1", pkg.Code, pkg.Version)
		}
	}
}

// assertOwnerSeed checks the row `migrate` wrote against ADR 0024 §1 and §6.
func assertOwnerSeed(t *testing.T, harness *testutil.APIHarness, email string) {
	t.Helper()

	owner := readOwner(t, harness)
	if owner.Email != strings.ToLower(email) {
		t.Fatalf("owner email = %q, want the canonical %q", owner.Email, strings.ToLower(email))
	}
	if owner.Role != entity.RoleOwner {
		t.Fatalf("owner role = %q, want owner", owner.Role)
	}
	if !owner.IsActive {
		t.Fatal("the seeded owner is not active")
	}

	cost, err := bcrypt.Cost([]byte(owner.PasswordHash))
	if err != nil {
		t.Fatalf("the stored value is not a bcrypt hash: %v", err)
	}
	if cost != adminservice.PasswordCost {
		t.Fatalf("bcrypt cost = %d, want %d", cost, adminservice.PasswordCost)
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(owner.PasswordHash),
		[]byte(testutil.AdminBootstrapEnv()["ADMIN_PASSWORD"]),
	); err != nil {
		t.Fatalf("the stored hash does not verify ADMIN_PASSWORD: %v", err)
	}
}

func readOwner(t *testing.T, harness *testutil.APIHarness) entity.Admin {
	t.Helper()

	var owner entity.Admin
	if err := harness.Postgres.Where("role = ?", entity.RoleOwner).First(&owner).Error; err != nil {
		t.Fatalf("read seeded owner: %v", err)
	}
	return owner
}
