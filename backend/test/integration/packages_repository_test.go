package integration

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	adminentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	adminrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// TestPackageRepositoryListsAndSeeds covers the persistence edge directly: the
// HTTP tests prove the endpoint, this proves the two repository behaviors the
// seed depends on (seed order and conflict-does-nothing) against real Postgres.
func TestPackageRepositoryListsAndSeeds(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	repo := repository.NewRepository(harness.Postgres, adminrepository.NewAuditRecorder(zap.NewNop()), zap.NewNop())
	ctx := context.Background()

	catalog, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(catalog) == 0 {
		t.Fatal("List returned an empty catalog; the seed should have run")
	}
	for index := 1; index < len(catalog); index++ {
		if catalog[index-1].ID >= catalog[index].ID {
			t.Fatalf("List is not in seed order: %d before %d", catalog[index-1].ID, catalog[index].ID)
		}
	}

	novel := entity.Package{
		Code:            "uji-repo",
		Name:            "Paket Uji",
		PriceIDR:        1000,
		DurationMinutes: 60,
		RateLimit:       "1M/1M",
	}
	inserted, err := repo.InsertMissing(ctx, []entity.Package{novel})
	if err != nil {
		t.Fatalf("InsertMissing: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("first InsertMissing inserted %d rows, want 1", inserted)
	}

	// A second run with different values must neither duplicate nor overwrite.
	novel.PriceIDR = 9999
	inserted, err = repo.InsertMissing(ctx, []entity.Package{novel})
	if err != nil {
		t.Fatalf("InsertMissing again: %v", err)
	}
	if inserted != 0 {
		t.Fatalf("re-run inserted %d rows, want 0", inserted)
	}

	var stored entity.Package
	if err := harness.Postgres.Where("code = ?", "uji-repo").First(&stored).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored.PriceIDR != 1000 {
		t.Fatalf("stored price = %d, want the originally seeded 1000", stored.PriceIDR)
	}

	if err := harness.Postgres.Where("code = ?", "uji-repo").Delete(&entity.Package{}).Error; err != nil {
		t.Fatalf("clean up: %v", err)
	}
}

// TestPackageRepositoryGetByID covers the typed read checkout depends on: the
// row comes back whole (price, duration, rate limit), and an unknown identity is
// the repository sentinel rather than a GORM error.
func TestPackageRepositoryGetByID(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	repo := repository.NewRepository(harness.Postgres, adminrepository.NewAuditRecorder(zap.NewNop()), zap.NewNop())
	ctx := context.Background()

	catalog, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(catalog) == 0 {
		t.Fatal("List returned an empty catalog; the seed should have run")
	}

	seeded := catalog[0]
	found, err := repo.GetByID(ctx, seeded.ID)
	if err != nil {
		t.Fatalf("GetByID(%d): %v", seeded.ID, err)
	}
	if found.Code != seeded.Code || found.PriceIDR != seeded.PriceIDR ||
		found.DurationMinutes != seeded.DurationMinutes || found.RateLimit != seeded.RateLimit {
		t.Fatalf("GetByID returned %+v, want the seeded row %+v", found, seeded)
	}

	if _, err := repo.GetByID(ctx, 999999); !errors.Is(err, repository.ErrPackageNotFound) {
		t.Fatalf("unknown Package error = %v, want repository.ErrPackageNotFound", err)
	}
}

// noopEntry is the audit fact a repository test does not read back; the row is
// still written, because the repository always records inside the transaction.
func noopEntry(action audit.Action, actor audit.Actor) audit.Entry {
	return audit.Entry{Actor: actor, Action: action, TargetType: audit.TargetPackage}
}

// TestPackageRepositoryWritesLeaveOneTrailRowEach exercises the persistence edge
// of ADR 0027 §2 directly: the change and its row commit together, and a refused
// write leaves no row behind.
func TestPackageRepositoryWritesLeaveOneTrailRowEach(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	recorder := adminrepository.NewAuditRecorder(zap.NewNop())
	repo := repository.NewRepository(harness.Postgres, recorder, zap.NewNop())
	ctx := context.Background()
	actor := panelActor(t, harness)

	created := entity.Package{
		Code: "uji-tulis", Name: "Paket Uji Tulis", PriceIDR: 3000,
		DurationMinutes: 120, RateLimit: "2M/2M", IsActive: true, Version: 1,
	}
	if err := repo.Create(ctx, &created, noopEntry(audit.ActionPackageCreate, actor)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("Create did not fill the new identity")
	}
	if rows := countAuditRows(t, harness); rows != 1 {
		t.Fatalf("audit rows after Create = %d, want 1", rows)
	}

	duplicate := created
	duplicate.ID = 0
	err := repo.Create(ctx, &duplicate, noopEntry(audit.ActionPackageCreate, actor))
	if !errors.Is(err, repository.ErrDuplicateCode) {
		t.Fatalf("duplicate code error = %v, want repository.ErrDuplicateCode", err)
	}
	if rows := countAuditRows(t, harness); rows != 1 {
		t.Fatalf("a refused Create left %d audit rows, want 1", rows)
	}

	edit := &entity.Package{
		ID: created.ID, Code: created.Code, Name: created.Name,
		PriceIDR: 4000, DurationMinutes: 120, RateLimit: "2M/2M",
	}
	staleEntry := func(before entity.Package) audit.Entry {
		return noopEntry(audit.ActionPackageUpdate, actor)
	}
	if _, err := repo.Update(ctx, edit, 99, staleEntry); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("stale update error = %v, want repository.ErrVersionConflict", err)
	}
	var conflict *repository.VersionConflictError
	if _, err := repo.Update(ctx, edit, 99, staleEntry); !errors.As(err, &conflict) {
		t.Fatalf("stale update error = %v, want the current row to travel with it", err)
	}
	if conflict.Current.ID != created.ID || conflict.Current.Version != 1 {
		t.Fatalf("conflict carried %+v, want the stored row at version 1", conflict.Current)
	}
	if rows := countAuditRows(t, harness); rows != 1 {
		t.Fatalf("a stale update left %d audit rows, want 1", rows)
	}

	updated, err := repo.Update(ctx, edit, 1, staleEntry)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Version != 2 || updated.PriceIDR != 4000 {
		t.Fatalf("updated row = %+v, want version 2 at 4000", updated)
	}
	if rows := countAuditRows(t, harness); rows != 2 {
		t.Fatalf("audit rows after Update = %d, want 2", rows)
	}
}

// TestPackageRepositoryRefusesToRetireTheLastActivePackage pins ADR 0025 §4 at the
// persistence edge, where the count is read and the row is locked.
func TestPackageRepositoryRefusesToRetireTheLastActivePackage(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	repo := repository.NewRepository(harness.Postgres, adminrepository.NewAuditRecorder(zap.NewNop()), zap.NewNop())
	ctx := context.Background()

	if err := harness.Postgres.Model(&entity.Package{}).
		Where("code <> ?", "harian").Update("is_active", false).Error; err != nil {
		t.Fatalf("retire the rest of the catalog: %v", err)
	}

	active, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].Code != "harian" {
		t.Fatalf("active catalog = %+v, want only harian", active)
	}

	all, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != len(active)+3 {
		t.Fatalf("admin catalog has %d rows, want the retired ones too", len(all))
	}

	before := active[0]
	_, err = repo.SetActive(ctx, before.ID, false, func(entity.Package) audit.Entry {
		return noopEntry(audit.ActionPackageDeactivate, panelActor(t, harness))
	})
	if !errors.Is(err, repository.ErrLastActivePackage) {
		t.Fatalf("retiring the last active Package = %v, want ErrLastActivePackage", err)
	}
	if rows := countAuditRows(t, harness); rows != 0 {
		t.Fatalf("a refused retirement left %d audit rows, want none", rows)
	}
}

// TestPackageRepositorySetActiveIsANoOpWhenNothingChanges keeps a repeated request
// from moving the version or leaving a trail row for an act that changed nothing.
func TestPackageRepositorySetActiveIsANoOpWhenNothingChanges(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	repo := repository.NewRepository(harness.Postgres, adminrepository.NewAuditRecorder(zap.NewNop()), zap.NewNop())
	ctx := context.Background()

	active, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	target := active[0]
	returned, err := repo.SetActive(ctx, target.ID, true, func(entity.Package) audit.Entry {
		return noopEntry(audit.ActionPackageActivate, panelActor(t, harness))
	})
	if err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if returned.Version != target.Version || !returned.IsActive {
		t.Fatalf("no-op returned %+v, want the row untouched at version %d", returned, target.Version)
	}
	if rows := countAuditRows(t, harness); rows != 0 {
		t.Fatalf("a no-op write left %d audit rows, want none", rows)
	}
}

func countAuditRows(t *testing.T, harness *testutil.APIHarness) int64 {
	t.Helper()

	var rows int64
	if err := harness.Postgres.Model(&adminentity.AuditEntry{}).Count(&rows).Error; err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return rows
}
