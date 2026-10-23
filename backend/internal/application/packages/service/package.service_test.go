package service_test

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
)

type fakeRepository struct {
	rows     []entity.Package
	listErr  error
	inserted int64
	seedErr  error
	seeds    [][]entity.Package
	byIDErr  error
}

func (r *fakeRepository) List(context.Context) ([]entity.Package, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.rows, nil
}

func (r *fakeRepository) GetByID(_ context.Context, id uint) (*entity.Package, error) {
	if r.byIDErr != nil {
		return nil, r.byIDErr
	}
	for index := range r.rows {
		if r.rows[index].ID == id {
			return &r.rows[index], nil
		}
	}
	return nil, repository.ErrPackageNotFound
}

func (r *fakeRepository) InsertMissing(_ context.Context, packages []entity.Package) (int64, error) {
	if r.seedErr != nil {
		return 0, r.seedErr
	}
	r.seeds = append(r.seeds, packages)
	return r.inserted, nil
}

func newService(repo *fakeRepository) service.Service {
	return service.NewService(repo, zap.NewNop())
}

func TestListPackages_MapsToTheFrozenLoginPageContract(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{rows: []entity.Package{
		{ID: 1, Code: "harian", Name: "Paket Harian", PriceIDR: 5000, DurationMinutes: 1440, RateLimit: "5M/5M"},
		{ID: 2, Code: "bulanan", Name: "Paket Bulanan", PriceIDR: 90000, DurationMinutes: 43200, RateLimit: "10M/10M"},
	}}

	packages, err := newService(repo).ListPackages(context.Background())
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}

	if len(packages) != 2 {
		t.Fatalf("got %d packages, want 2", len(packages))
	}
	first := packages[0]
	if first.ID != 1 || first.Name != "Paket Harian" {
		t.Fatalf("identity = %+v", first)
	}
	if first.Price != "Rp 5.000" {
		t.Fatalf("harga = %q, want %q", first.Price, "Rp 5.000")
	}
	if first.ActivePeriod != "1 Hari" {
		t.Fatalf("durasiActivePeriod = %q, want %q", first.ActivePeriod, "1 Hari")
	}
	if first.SpeedLimit != "5M" {
		t.Fatalf("batasKecepatan = %q, want %q", first.SpeedLimit, "5M")
	}
	if packages[1].Price != "Rp 90.000" || packages[1].ActivePeriod != "30 Hari" {
		t.Fatalf("second package = %+v", packages[1])
	}
}

func TestListPackages_EmptyCatalogIsAnEmptyListNotEmptyNull(t *testing.T) {
	t.Parallel()

	packages, err := newService(&fakeRepository{}).ListPackages(context.Background())
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}
	if packages == nil {
		t.Fatal("empty catalog must map to an empty slice so the response is [] not null")
	}
	if len(packages) != 0 {
		t.Fatalf("got %d packages, want 0", len(packages))
	}
}

func TestListPackages_RepositoryFailureIsReturned(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{listErr: errors.New("boom")}
	if _, err := newService(repo).ListPackages(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestGetPackage_ReturnsTheTypedRowCheckoutNeeds(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{rows: []entity.Package{
		{ID: 7, Code: "mingguan", Name: "Paket Mingguan", PriceIDR: 25000, DurationMinutes: 10080, RateLimit: "10M/10M"},
	}}

	pkg, err := newService(repo).GetPackage(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if pkg.PriceIDR != 25000 || pkg.DurationMinutes != 10080 || pkg.RateLimit != "10M/10M" {
		t.Fatalf("typed values lost: %+v", pkg)
	}
}

func TestGetPackage_UnknownPackageIsTheServiceSentinel(t *testing.T) {
	t.Parallel()

	_, err := newService(&fakeRepository{}).GetPackage(context.Background(), 99)
	if !errors.Is(err, service.ErrPackageNotFound) {
		t.Fatalf("err = %v, want service.ErrPackageNotFound", err)
	}
}

func TestGetPackage_RepositoryFailureIsWrapped(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{byIDErr: errors.New("boom")}
	_, err := newService(repo).GetPackage(context.Background(), 1)
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, service.ErrPackageNotFound) {
		t.Fatal("a database failure must not look like a missing Package")
	}
}

func TestSeed_InsertsTheStarterCatalogOnce(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{inserted: 4}
	svc := newService(repo)

	inserted, err := svc.Seed(context.Background())
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if inserted != 4 {
		t.Fatalf("inserted = %d, want 4", inserted)
	}
	if len(repo.seeds) != 1 {
		t.Fatalf("repo called %d times, want 1", len(repo.seeds))
	}

	seeded := repo.seeds[0]
	if len(seeded) != len(service.StarterCatalog()) {
		t.Fatalf("seeded %d packages, want %d", len(seeded), len(service.StarterCatalog()))
	}
	seenCode := make(map[string]bool, len(seeded))
	for _, pkg := range seeded {
		if pkg.Code == "" {
			t.Fatalf("seeded package %q has no code; the seed would not be idempotent", pkg.Name)
		}
		if seenCode[pkg.Code] {
			t.Fatalf("duplicate seed code %q", pkg.Code)
		}
		seenCode[pkg.Code] = true
		if pkg.Name == "" || pkg.PriceIDR <= 0 || pkg.DurationMinutes <= 0 || pkg.RateLimit == "" {
			t.Fatalf("seeded package %+v is incomplete", pkg)
		}
	}
}

func TestSeed_SecondRunInsertsNothing(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{inserted: 0}
	inserted, err := newService(repo).Seed(context.Background())
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if inserted != 0 {
		t.Fatalf("inserted = %d on a re-run, want 0", inserted)
	}
}

func TestSeed_RepositoryFailureIsReturned(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{seedErr: errors.New("boom")}
	if _, err := newService(repo).Seed(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStarterCatalog_ReturnsAFreshSlice(t *testing.T) {
	t.Parallel()

	first := service.StarterCatalog()
	first[0].Name = "mutated"
	if service.StarterCatalog()[0].Name == "mutated" {
		t.Fatal("StarterCatalog shares its backing array between calls")
	}
}

func TestFormatRupiah(t *testing.T) {
	t.Parallel()

	cases := map[int64]string{
		0:       "Rp 0",
		500:     "Rp 500",
		5000:    "Rp 5.000",
		90000:   "Rp 90.000",
		125000:  "Rp 125.000",
		1000000: "Rp 1.000.000",
	}
	for amount, want := range cases {
		if got := service.FormatRupiah(amount); got != want {
			t.Errorf("FormatRupiah(%d) = %q, want %q", amount, got, want)
		}
	}
}

func TestFormatActivePeriod(t *testing.T) {
	t.Parallel()

	cases := map[int]string{
		0:     "0 Menit",
		45:    "45 Menit",
		60:    "1 Jam",
		720:   "12 Jam",
		1440:  "1 Hari",
		43200: "30 Hari",
	}
	for minutes, want := range cases {
		if got := service.FormatActivePeriod(minutes); got != want {
			t.Errorf("FormatActivePeriod(%d) = %q, want %q", minutes, got, want)
		}
	}
}

func TestFormatSpeedLimit(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"5M/5M":   "5M",
		"10M/5M":  "10M",
		"5M":      "5M",
		" 1M/1M ": "1M",
	}
	for rateLimit, want := range cases {
		if got := service.FormatSpeedLimit(rateLimit); got != want {
			t.Errorf("FormatSpeedLimit(%q) = %q, want %q", rateLimit, got, want)
		}
	}
}
