package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
)

func owner() audit.Actor {
	return audit.Actor{ID: 1, Email: "owner@example.test", IP: "203.0.113.9"}
}

func validCreateRequest() *dto.CreatePackageRequest {
	return &dto.CreatePackageRequest{
		Code:            "paket-malam",
		Name:            "  Paket Malam  ",
		PriceIDR:        7000,
		DurationMinutes: 360,
		RateLimit:       "5M/5M",
	}
}

func validUpdateRequest() *dto.UpdatePackageRequest {
	return &dto.UpdatePackageRequest{
		Name:            "Paket Harian",
		PriceIDR:        7500,
		DurationMinutes: 1440,
		RateLimit:       "5M/5M",
		Version:         3,
		Code:            "harian",
	}
}

func seedHarian() []entity.Package {
	return []entity.Package{{
		ID: 7, Code: "harian", Name: "Paket Harian", PriceIDR: 5000,
		DurationMinutes: 1440, RateLimit: "5M/5M", IsActive: true, Version: 3,
	}}
}

// assertFieldRefused proves a rule refused the field named, which is what the
// panel maps back onto a form input.
func assertFieldRefused(t *testing.T, err error, field string) {
	t.Helper()

	if !errors.Is(err, service.ErrPackageInvalid) {
		t.Fatalf("error = %v, want ErrPackageInvalid", err)
	}
	var fieldError *service.FieldError
	if !errors.As(err, &fieldError) {
		t.Fatalf("error = %v, want a *FieldError", err)
	}
	if fieldError.Field != field {
		t.Fatalf("refused field = %q, want %q", fieldError.Field, field)
	}
	if fieldError.Reason == "" {
		t.Fatal("a refused field must carry a reason the panel can show")
	}
}

func TestCreatePackage_StoresTheValidatedValuesAndOneTrailRow(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	created, err := newService(repo).CreatePackage(context.Background(), owner(), validCreateRequest())
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}

	if len(repo.created) != 1 {
		t.Fatalf("repo stored %d packages, want 1", len(repo.created))
	}
	stored := repo.created[0]
	if stored.Code != "paket-malam" || stored.Name != "Paket Malam" {
		t.Fatalf("stored identity = %q/%q, want trimmed input", stored.Code, stored.Name)
	}
	if stored.PriceIDR != 7000 || stored.DurationMinutes != 360 || stored.RateLimit != "5M/5M" {
		t.Fatalf("stored values = %+v", stored)
	}
	if !stored.IsActive || stored.Version != 1 {
		t.Fatalf("new package is_active=%v version=%d, want true/1", stored.IsActive, stored.Version)
	}

	if created.ID != 42 || created.Version != 1 || !created.IsActive {
		t.Fatalf("response = %+v, want the stored row", created)
	}

	if repo.createEntry.Action != audit.ActionPackageCreate {
		t.Fatalf("audit action = %q, want %q", repo.createEntry.Action, audit.ActionPackageCreate)
	}
	createPayload, ok := repo.createEntry.Payload["after"].(map[string]any)
	if !ok || createPayload["price_idr"] != int64(7000) {
		t.Fatalf("audit payload lost the created values: %+v", repo.createEntry.Payload)
	}
	if repo.createEntry.Actor.Email != owner().Email {
		t.Fatalf("audit actor = %q, want %q", repo.createEntry.Actor.Email, owner().Email)
	}
}

func TestCreatePackage_RefusesADuplicateCodeAsAConflict(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{createErr: repository.ErrDuplicateCode}
	_, err := newService(repo).CreatePackage(context.Background(), owner(), validCreateRequest())
	if !errors.Is(err, service.ErrPackageCodeTaken) {
		t.Fatalf("error = %v, want ErrPackageCodeTaken", err)
	}
}

func TestCreatePackage_WrapsAnUnexpectedRepositoryFailure(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{createErr: errors.New("postgres is down")}
	_, err := newService(repo).CreatePackage(context.Background(), owner(), validCreateRequest())
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, service.ErrPackageCodeTaken) || errors.Is(err, service.ErrPackageInvalid) {
		t.Fatalf("a database failure must not look like a refusal: %v", err)
	}
}

func TestAuditedWritesRefuseAMissingActor(t *testing.T) {
	t.Parallel()

	svc := newService(&fakeRepository{})
	writes := map[string]error{
		"create": func() error {
			_, err := svc.CreatePackage(context.Background(), audit.Actor{}, validCreateRequest())
			return err
		}(),
		"update": func() error {
			_, err := svc.UpdatePackage(context.Background(), audit.Actor{}, 7, validUpdateRequest())
			return err
		}(),
		"set active": func() error {
			_, err := svc.SetActive(context.Background(), audit.Actor{}, 7, &dto.SetActiveRequest{Active: boolPointer(false)})
			return err
		}(),
	}
	for name, err := range writes {
		if !errors.Is(err, service.ErrActorRequired) {
			t.Fatalf("%s error = %v, want ErrActorRequired", name, err)
		}
	}
}

func TestCreatePackage_FieldRulesAtEveryBoundary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		field  string
		mutate func(*dto.CreatePackageRequest)
	}{
		{name: "name one character is enough", mutate: func(r *dto.CreatePackageRequest) { r.Name = "P" }},
		{name: "name at 80 characters", mutate: func(r *dto.CreatePackageRequest) { r.Name = strings.Repeat("a", 80) }},
		{name: "name over 80 characters", field: "name", mutate: func(r *dto.CreatePackageRequest) {
			r.Name = strings.Repeat("a", 81)
		}},
		{name: "name empty after trim", field: "name", mutate: func(r *dto.CreatePackageRequest) { r.Name = "   " }},
		{name: "name with a control character", field: "name", mutate: func(r *dto.CreatePackageRequest) {
			r.Name = "Paket\nHarian"
		}},
		{name: "price one rupiah", mutate: func(r *dto.CreatePackageRequest) { r.PriceIDR = 1 }},
		{name: "price at the ceiling", mutate: func(r *dto.CreatePackageRequest) { r.PriceIDR = 100_000_000 }},
		{name: "price zero", field: "price_idr", mutate: func(r *dto.CreatePackageRequest) { r.PriceIDR = 0 }},
		{name: "price negative", field: "price_idr", mutate: func(r *dto.CreatePackageRequest) { r.PriceIDR = -1 }},
		{name: "price above the ceiling", field: "price_idr", mutate: func(r *dto.CreatePackageRequest) {
			r.PriceIDR = 100_000_001
		}},
		{name: "duration one minute", mutate: func(r *dto.CreatePackageRequest) { r.DurationMinutes = 1 }},
		{name: "duration at 366 days", mutate: func(r *dto.CreatePackageRequest) { r.DurationMinutes = 527_040 }},
		{name: "duration zero", field: "duration_minutes", mutate: func(r *dto.CreatePackageRequest) {
			r.DurationMinutes = 0
		}},
		{name: "duration negative", field: "duration_minutes", mutate: func(r *dto.CreatePackageRequest) {
			r.DurationMinutes = -1
		}},
		{name: "duration past 366 days", field: "duration_minutes", mutate: func(r *dto.CreatePackageRequest) {
			r.DurationMinutes = 527_041
		}},
		{name: "rate limit bare numbers", mutate: func(r *dto.CreatePackageRequest) { r.RateLimit = "1/2" }},
		{name: "rate limit kilo suffix", mutate: func(r *dto.CreatePackageRequest) { r.RateLimit = "512k/1M" }},
		{name: "rate limit decimal", mutate: func(r *dto.CreatePackageRequest) { r.RateLimit = "1.5M/1.5M" }},
		{name: "rate limit without the upload side", field: "rate_limit", mutate: func(r *dto.CreatePackageRequest) {
			r.RateLimit = "5M"
		}},
		{name: "rate limit with a space", field: "rate_limit", mutate: func(r *dto.CreatePackageRequest) {
			r.RateLimit = "5M / 5M"
		}},
		{name: "rate limit empty", field: "rate_limit", mutate: func(r *dto.CreatePackageRequest) { r.RateLimit = "" }},
		{name: "code with a capital", field: "code", mutate: func(r *dto.CreatePackageRequest) { r.Code = "Harian" }},
		{name: "code with an underscore", field: "code", mutate: func(r *dto.CreatePackageRequest) { r.Code = "paket_harian" }},
		{name: "code leading dash", field: "code", mutate: func(r *dto.CreatePackageRequest) { r.Code = "-harian" }},
		{name: "code trailing dash", field: "code", mutate: func(r *dto.CreatePackageRequest) { r.Code = "harian-" }},
		{name: "code double dash", field: "code", mutate: func(r *dto.CreatePackageRequest) { r.Code = "paket--malam" }},
		{name: "code empty", field: "code", mutate: func(r *dto.CreatePackageRequest) { r.Code = "" }},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := validCreateRequest()
			testCase.mutate(request)
			repo := &fakeRepository{}

			_, err := newService(repo).CreatePackage(context.Background(), owner(), request)
			if testCase.field == "" {
				if err != nil {
					t.Fatalf("valid value refused: %v", err)
				}
				return
			}
			assertFieldRefused(t, err, testCase.field)
			if len(repo.created) != 0 {
				t.Fatal("a refused field must not reach the repository")
			}
		})
	}
}

func TestUpdatePackage_BumpsTheVersionAndTrailsOnlyWhatChanged(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{rows: seedHarian(), updateBefore: seedHarian()[0]}
	updated, err := newService(repo).UpdatePackage(context.Background(), owner(), 7, validUpdateRequest())
	if err != nil {
		t.Fatalf("UpdatePackage: %v", err)
	}

	if repo.updateVersion != 3 {
		t.Fatalf("version presented to the repository = %d, want 3", repo.updateVersion)
	}
	if updated.Version != 4 || updated.PriceIDR != 7500 {
		t.Fatalf("response = %+v, want version 4 at the new price", updated)
	}

	payload := repo.updateEntry.Payload
	if payload["price_idr"] == nil {
		t.Fatalf("audit payload lost the price change: %+v", payload)
	}
	if _, ok := payload["name"]; ok {
		t.Fatalf("audit payload records an unchanged name: %+v", payload)
	}
	versionChange := payload["version"].(map[string]any)
	if versionChange["before"] != 3 || versionChange["after"] != 4 {
		t.Fatalf("audit version change = %+v, want 3→4", versionChange)
	}
	if repo.updateEntry.Action != audit.ActionPackageUpdate {
		t.Fatalf("audit action = %q, want %q", repo.updateEntry.Action, audit.ActionPackageUpdate)
	}
}

func TestUpdatePackage_RefusesARenameOfTheImmutableCode(t *testing.T) {
	t.Parallel()

	request := validUpdateRequest()
	request.Code = "harian-baru"
	repo := &fakeRepository{rows: seedHarian()}

	_, err := newService(repo).UpdatePackage(context.Background(), owner(), 7, request)
	if !errors.Is(err, service.ErrPackageCodeImmutable) {
		t.Fatalf("error = %v, want ErrPackageCodeImmutable", err)
	}
	if repo.updateVersion != 0 {
		t.Fatal("an immutable-code refusal must not reach the repository write")
	}
}

func TestUpdatePackage_AcceptsTheCurrentCodeEchoedBack(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{rows: seedHarian(), updateBefore: seedHarian()[0]}
	if _, err := newService(repo).UpdatePackage(context.Background(), owner(), 7, validUpdateRequest()); err != nil {
		t.Fatalf("echoing the current code must be accepted: %v", err)
	}
}

func TestUpdatePackage_MapsAStaleVersionToTheCurrentRow(t *testing.T) {
	t.Parallel()

	stale := seedHarian()[0]
	stale.Version = 9
	stale.PriceIDR = 9000
	repo := &fakeRepository{rows: seedHarian(), updateErr: &repository.VersionConflictError{Current: stale}}

	_, err := newService(repo).UpdatePackage(context.Background(), owner(), 7, validUpdateRequest())
	if !errors.Is(err, service.ErrPackageVersionConflict) {
		t.Fatalf("error = %v, want ErrPackageVersionConflict", err)
	}
	var conflict *service.VersionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %v, want the current row to travel with it", err)
	}
	if conflict.Current.Version != 9 || conflict.Current.PriceIDR != 9000 {
		t.Fatalf("current row = %+v, want version 9 at 9000", conflict.Current)
	}
}

func TestUpdatePackage_UnknownPackageIsTheServiceSentinel(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	_, err := newService(repo).UpdatePackage(context.Background(), owner(), 99, validUpdateRequest())
	if !errors.Is(err, service.ErrPackageNotFound) {
		t.Fatalf("error = %v, want ErrPackageNotFound", err)
	}
}

func TestUpdatePackage_RequiresAVersion(t *testing.T) {
	t.Parallel()

	request := validUpdateRequest()
	request.Version = 0
	_, err := newService(&fakeRepository{rows: seedHarian()}).UpdatePackage(context.Background(), owner(), 7, request)
	assertFieldRefused(t, err, "version")
}

func TestSetActive_RetiresAndTrailsTheAct(t *testing.T) {
	t.Parallel()

	before := seedHarian()[0]
	repo := &fakeRepository{setActiveBefore: before}

	updated, err := newService(repo).SetActive(
		context.Background(), owner(), 7, &dto.SetActiveRequest{Active: boolPointer(false)})
	if err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	if repo.setActiveID != 7 || repo.setActiveArg {
		t.Fatalf("repository called with id=%d active=%v, want 7/false", repo.setActiveID, repo.setActiveArg)
	}
	if updated.IsActive || updated.Version != before.Version+1 {
		t.Fatalf("response = %+v, want inactive at version %d", updated, before.Version+1)
	}
	if repo.setActiveEntry.Action != audit.ActionPackageDeactivate {
		t.Fatalf("audit action = %q, want %q", repo.setActiveEntry.Action, audit.ActionPackageDeactivate)
	}
	change := repo.setActiveEntry.Payload["is_active"].(map[string]any)
	if change["before"] != true || change["after"] != false {
		t.Fatalf("audit is_active change = %+v, want true→false", change)
	}
}

func TestSetActive_ReactivationHasItsOwnAction(t *testing.T) {
	t.Parallel()

	before := seedHarian()[0]
	before.IsActive = false
	repo := &fakeRepository{setActiveBefore: before}

	if _, err := newService(repo).SetActive(
		context.Background(), owner(), 7, &dto.SetActiveRequest{Active: boolPointer(true)}); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if repo.setActiveEntry.Action != audit.ActionPackageActivate {
		t.Fatalf("audit action = %q, want %q", repo.setActiveEntry.Action, audit.ActionPackageActivate)
	}
}

func TestSetActive_RefusesToRetireTheLastActivePackage(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{setActiveErr: repository.ErrLastActivePackage}
	_, err := newService(repo).SetActive(
		context.Background(), owner(), 7, &dto.SetActiveRequest{Active: boolPointer(false)})
	if !errors.Is(err, service.ErrLastActivePackage) {
		t.Fatalf("error = %v, want ErrLastActivePackage", err)
	}
}

func TestSetActive_RequiresTheFlag(t *testing.T) {
	t.Parallel()

	_, err := newService(&fakeRepository{}).SetActive(context.Background(), owner(), 7, &dto.SetActiveRequest{})
	assertFieldRefused(t, err, "active")
}

func TestListPackages_ReadsOnlyTheActiveCatalog(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		rows: []entity.Package{{
			ID: 1, Code: "harian", Name: "Paket Harian", PriceIDR: 5000, DurationMinutes: 1440,
			RateLimit: "5M/5M", IsActive: true, Version: 1,
		}},
		all: []entity.Package{
			{
				ID: 1, Code: "harian", Name: "Paket Harian", PriceIDR: 5000, DurationMinutes: 1440,
				RateLimit: "5M/5M", IsActive: true, Version: 1,
			},
			{
				ID: 2, Code: "lama", Name: "Paket Lama", PriceIDR: 1000, DurationMinutes: 60,
				RateLimit: "1M/1M", IsActive: false, Version: 2,
			},
		},
	}
	svc := newService(repo)

	public, err := svc.ListPackages(context.Background())
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}
	if len(public) != 1 || public[0].ID != 1 {
		t.Fatalf("public catalog = %+v, want only the active Package", public)
	}

	admin, err := svc.ListAdminPackages(context.Background())
	if err != nil {
		t.Fatalf("ListAdminPackages: %v", err)
	}
	if len(admin) != 2 {
		t.Fatalf("admin catalog has %d rows, want the retired row too", len(admin))
	}
	if admin[1].IsActive || admin[1].Version != 2 || admin[1].Code != "lama" {
		t.Fatalf("retired row = %+v, want is_active=false version=2", admin[1])
	}
}

func TestListAdminPackages_RepositoryFailureIsReturned(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{allErr: fmt.Errorf("boom")}
	if _, err := newService(repo).ListAdminPackages(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func boolPointer(value bool) *bool { return &value }
