package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	adminrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
)

type fakeRepository struct {
	admins    int64
	countErr  error
	inserted  int64
	insertErr error
	created   []entity.Admin

	// CreateAdmin's side: what the service asked to write, and the trail facts it
	// asked to record with it.
	createErr error
	written   []entity.Admin
	entries   []audit.Entry
}

func (r *fakeRepository) CountAdmins(context.Context) (int64, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	return r.admins, nil
}

func (r *fakeRepository) InsertOwner(_ context.Context, admin *entity.Admin) (int64, error) {
	if r.insertErr != nil {
		return 0, r.insertErr
	}
	r.created = append(r.created, *admin)
	return r.inserted, nil
}

// CreateAdmin captures the write and its audit fact, and hands back an id the way
// the real insert does, so the service can build its response.
func (r *fakeRepository) CreateAdmin(_ context.Context, admin *entity.Admin, entry audit.Entry) error {
	if r.createErr != nil {
		return r.createErr
	}
	admin.ID = uint(len(r.written) + 1)
	r.written = append(r.written, *admin)
	r.entries = append(r.entries, entry)
	return nil
}

func newService(repo *fakeRepository, settings service.BootstrapSettings) service.Service {
	return service.NewService(repo, settings, zap.NewNop())
}

// TestBootstrapOwnerSeedsTheFirstOwner is the acceptance criterion: a deployment
// with an empty admins table and a bootstrap pair gets exactly one active owner.
func TestBootstrapOwnerSeedsTheFirstOwner(t *testing.T) {
	repo := &fakeRepository{inserted: 1}
	svc := newService(repo, service.BootstrapSettings{
		Email:    "  Owner@Example.TEST ",
		Password: "kata-sandi-yang-cukup-panjang",
	})

	inserted, err := svc.BootstrapOwner(context.Background())
	if err != nil {
		t.Fatalf("BootstrapOwner: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("inserted = %d, want 1", inserted)
	}
	if len(repo.created) != 1 {
		t.Fatalf("repository created %d rows, want 1", len(repo.created))
	}

	created := repo.created[0]
	if created.Email != "owner@example.test" {
		t.Fatalf("email = %q, want the canonical lowercase form", created.Email)
	}
	if created.Role != entity.RoleOwner {
		t.Fatalf("role = %q, want owner", created.Role)
	}
	if !created.IsActive {
		t.Fatal("the seeded owner must be active")
	}

	cost, err := bcrypt.Cost([]byte(created.PasswordHash))
	if err != nil {
		t.Fatalf("the stored value is not a bcrypt hash: %v", err)
	}
	if cost != service.PasswordCost {
		t.Fatalf("bcrypt cost = %d, want %d", cost, service.PasswordCost)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(created.PasswordHash), []byte("kata-sandi-yang-cukup-panjang")); err != nil {
		t.Fatalf("the stored hash does not verify the configured password: %v", err)
	}
}

// TestBootstrapOwnerIgnoresTheEnvironmentOnceAnAdminExists is ADR 0024 §6: a
// deploy must never reset a password, and it must not fail on a pair it is not
// allowed to use either.
func TestBootstrapOwnerIgnoresTheEnvironmentOnceAnAdminExists(t *testing.T) {
	for name, settings := range map[string]service.BootstrapSettings{
		"different pair":   {Email: "other@example.test", Password: "another-long-password"},
		"empty pair":       {},
		"invalid pair":     {Email: "not-an-email", Password: "short"},
		"half-filled pair": {Email: "owner@example.test"},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepository{admins: 1}
			svc := newService(repo, settings)

			inserted, err := svc.BootstrapOwner(context.Background())
			if err != nil {
				t.Fatalf("BootstrapOwner with an existing admin: %v", err)
			}
			if inserted != 0 {
				t.Fatalf("inserted = %d, want 0", inserted)
			}
			if len(repo.created) != 0 {
				t.Fatalf("repository created %d rows, want none", len(repo.created))
			}
		})
	}
}

// TestBootstrapOwnerRefusesToLeaveThePanelWithoutADoor names the variables an
// operator has to set: the message is the whole diagnostic of a failed deploy.
func TestBootstrapOwnerRefusesToLeaveThePanelWithoutADoor(t *testing.T) {
	cases := map[string]struct {
		settings service.BootstrapSettings
		want     string
	}{
		"nothing configured": {
			settings: service.BootstrapSettings{},
			want:     "ADMIN_EMAIL and ADMIN_PASSWORD",
		},
		"only an email": {
			settings: service.BootstrapSettings{Email: "owner@example.test"},
			want:     "ADMIN_PASSWORD",
		},
		"only a password": {
			settings: service.BootstrapSettings{Password: "kata-sandi-yang-cukup-panjang"},
			want:     "ADMIN_EMAIL",
		},
		"whitespace only": {
			settings: service.BootstrapSettings{Email: "   ", Password: ""},
			want:     "ADMIN_EMAIL",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepository{}
			svc := newService(repo, testCase.settings)

			inserted, err := svc.BootstrapOwner(context.Background())
			if !errors.Is(err, service.ErrBootstrapCredentialsMissing) {
				t.Fatalf("error = %v, want ErrBootstrapCredentialsMissing", err)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error %q does not name %q", err, testCase.want)
			}
			if inserted != 0 {
				t.Fatalf("inserted = %d, want 0", inserted)
			}
			if len(repo.created) != 0 {
				t.Fatalf("repository created %d rows, want none", len(repo.created))
			}
		})
	}
}

// TestBootstrapOwnerValidatesTheBootstrapPair keeps the seed from creating an
// owner the panel's own rules would refuse.
func TestBootstrapOwnerValidatesTheBootstrapPair(t *testing.T) {
	cases := map[string]struct {
		settings service.BootstrapSettings
		want     error
	}{
		"email without a domain": {
			settings: service.BootstrapSettings{Email: "owner@", Password: "kata-sandi-yang-cukup-panjang"},
			want:     service.ErrBootstrapEmailInvalid,
		},
		"email with a space": {
			settings: service.BootstrapSettings{Email: "own er@example.test", Password: "kata-sandi-yang-cukup-panjang"},
			want:     service.ErrBootstrapEmailInvalid,
		},
		"email with two at signs": {
			settings: service.BootstrapSettings{Email: "owner@example@test", Password: "kata-sandi-yang-cukup-panjang"},
			want:     service.ErrBootstrapEmailInvalid,
		},
		"short password": {
			settings: service.BootstrapSettings{Email: "owner@example.test", Password: "pendek"},
			want:     service.ErrBootstrapPasswordWeak,
		},
		"password longer than bcrypt accepts": {
			settings: service.BootstrapSettings{Email: "owner@example.test", Password: strings.Repeat("a", 73)},
			want:     service.ErrBootstrapPasswordWeak,
		},
		"password is the email": {
			settings: service.BootstrapSettings{Email: "owner@example.test", Password: "Owner@Example.test"},
			want:     service.ErrBootstrapPasswordWeak,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepository{}
			svc := newService(repo, testCase.settings)

			if _, err := svc.BootstrapOwner(context.Background()); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
			if len(repo.created) != 0 {
				t.Fatalf("repository created %d rows, want none", len(repo.created))
			}
		})
	}
}

// TestBootstrapOwnerReportsInsertedRows pins insert-if-missing: when a
// concurrent deploy already wrote the owner, this one adds nothing and still
// reports success.
func TestBootstrapOwnerReportsInsertedRows(t *testing.T) {
	repo := &fakeRepository{inserted: 0}
	svc := newService(repo, service.BootstrapSettings{
		Email:    "owner@example.test",
		Password: "kata-sandi-yang-cukup-panjang",
	})

	inserted, err := svc.BootstrapOwner(context.Background())
	if err != nil {
		t.Fatalf("BootstrapOwner: %v", err)
	}
	if inserted != 0 {
		t.Fatalf("inserted = %d, want 0", inserted)
	}
}

// TestBootstrapOwnerPropagatesRepositoryFailures keeps a database blip from
// looking like a successful seed.
func TestBootstrapOwnerPropagatesRepositoryFailures(t *testing.T) {
	countFailure := &fakeRepository{countErr: errors.New("postgres is down")}
	if _, err := newService(countFailure, service.BootstrapSettings{
		Email:    "owner@example.test",
		Password: "kata-sandi-yang-cukup-panjang",
	}).BootstrapOwner(context.Background()); err == nil {
		t.Fatal("a failing count must not look like a seed")
	}

	insertFailure := &fakeRepository{insertErr: errors.New("unique violation")}
	if _, err := newService(insertFailure, service.BootstrapSettings{
		Email:    "owner@example.test",
		Password: "kata-sandi-yang-cukup-panjang",
	}).BootstrapOwner(context.Background()); err == nil {
		t.Fatal("a failing insert must not look like a seed")
	}
}

// panelOwner is the actor every audited write in these tests comes from: the
// owner who is signed in, never the admin being created.
func panelOwner() audit.Actor {
	return audit.Actor{ID: 7, Email: "owner@example.test", IP: "203.0.113.9"}
}

// TestCreateAdminWritesOneActiveAdminAndExactlyOneAuditRow is #30's acceptance
// criterion at the service boundary: one write, one trail row, and the row carries
// the actor, the action and no credential.
func TestCreateAdminWritesOneActiveAdminAndExactlyOneAuditRow(t *testing.T) {
	repo := &fakeRepository{}
	svc := newService(repo, service.BootstrapSettings{})

	response, err := svc.CreateAdmin(context.Background(), panelOwner(), &dto.CreateAdminRequest{
		Email:    " Support@Example.TEST ",
		Role:     " Support ",
		Password: "kata-sandi-yang-cukup-panjang",
	})
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	if len(repo.written) != 1 {
		t.Fatalf("repository wrote %d admins, want 1", len(repo.written))
	}
	written := repo.written[0]
	if written.Email != "support@example.test" {
		t.Fatalf("email = %q, want the canonical lowercase form", written.Email)
	}
	if written.Role != entity.RoleSupport {
		t.Fatalf("role = %q, want support", written.Role)
	}
	if !written.IsActive {
		t.Fatal("a newly created admin must be active")
	}
	cost, err := bcrypt.Cost([]byte(written.PasswordHash))
	if err != nil {
		t.Fatalf("the stored value is not a bcrypt hash: %v", err)
	}
	if cost != service.PasswordCost {
		t.Fatalf("bcrypt cost = %d, want %d", cost, service.PasswordCost)
	}

	if response.Email != written.Email || response.Role != string(written.Role) || !response.IsActive {
		t.Fatalf("response = %+v, want the admin that was written", response)
	}
	if response.ID == 0 {
		t.Fatal("response carries no id")
	}

	if len(repo.entries) != 1 {
		t.Fatalf("audit rows = %d, want exactly 1", len(repo.entries))
	}
	entry := repo.entries[0]
	if entry.Action != audit.ActionAdminCreate {
		t.Fatalf("action = %q, want %q", entry.Action, audit.ActionAdminCreate)
	}
	if entry.TargetType != audit.TargetAdmin {
		t.Fatalf("target type = %q, want %q", entry.TargetType, audit.TargetAdmin)
	}
	if entry.Actor != panelOwner() {
		t.Fatalf("actor = %+v, want %+v", entry.Actor, panelOwner())
	}
	if entry.Payload["email"] != "support@example.test" || entry.Payload["role"] != string(entity.RoleSupport) {
		t.Fatalf("payload = %v, want the fields the owner chose", entry.Payload)
	}
	assertNoCredentialInPayload(t, entry.Payload)
}

// assertNoCredentialInPayload keeps a trail row from becoming a place a password
// or its hash can leak (ADR 0027 §4).
func assertNoCredentialInPayload(t *testing.T, payload map[string]any) {
	t.Helper()

	for _, forbidden := range []string{"password", "password_hash", "hash"} {
		if _, found := payload[forbidden]; found {
			t.Fatalf("payload carries %q", forbidden)
		}
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	if strings.Contains(strings.ToLower(string(encoded)), "kata-sandi") {
		t.Fatalf("payload %s carries the plaintext password", encoded)
	}
}

// TestCreateAdminRefusesBeforeAnyWrite keeps every rule in the service: a refused
// request reaches the database neither as an admin nor as a trail row.
func TestCreateAdminRefusesBeforeAnyWrite(t *testing.T) {
	cases := map[string]struct {
		request *dto.CreateAdminRequest
		want    error
	}{
		"role the matrix does not know": {
			request: &dto.CreateAdminRequest{
				Email:    "new@example.test",
				Role:     "superuser",
				Password: "kata-sandi-yang-cukup-panjang",
			},
			want: service.ErrRoleInvalid,
		},
		"empty role": {
			request: &dto.CreateAdminRequest{
				Email:    "new@example.test",
				Password: "kata-sandi-yang-cukup-panjang",
			},
			want: service.ErrRoleInvalid,
		},
		"email without a domain": {
			request: &dto.CreateAdminRequest{
				Email:    "new@",
				Role:     "support",
				Password: "kata-sandi-yang-cukup-panjang",
			},
			want: service.ErrEmailInvalid,
		},
		"short password": {
			request: &dto.CreateAdminRequest{
				Email:    "new@example.test",
				Role:     "support",
				Password: "pendek",
			},
			want: service.ErrPasswordWeak,
		},
		"password longer than bcrypt accepts": {
			request: &dto.CreateAdminRequest{
				Email:    "new@example.test",
				Role:     "support",
				Password: strings.Repeat("a", 73),
			},
			want: service.ErrPasswordWeak,
		},
		"password is the email": {
			request: &dto.CreateAdminRequest{
				Email:    "new@example.test",
				Role:     "support",
				Password: "New@Example.test",
			},
			want: service.ErrPasswordWeak,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepository{}
			svc := newService(repo, service.BootstrapSettings{})

			if _, err := svc.CreateAdmin(context.Background(), panelOwner(), testCase.request); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
			if len(repo.written) != 0 || len(repo.entries) != 0 {
				t.Fatalf("a refused write stored %d admins and %d audit rows, want none",
					len(repo.written), len(repo.entries))
			}
		})
	}
}

// TestCreateAdminNeedsItsActor refuses an unattributed write: a row with no actor
// cannot answer the question the trail exists for.
func TestCreateAdminNeedsItsActor(t *testing.T) {
	repo := &fakeRepository{}
	svc := newService(repo, service.BootstrapSettings{})

	_, err := svc.CreateAdmin(context.Background(), audit.Actor{}, &dto.CreateAdminRequest{
		Email:    "new@example.test",
		Role:     "support",
		Password: "kata-sandi-yang-cukup-panjang",
	})
	if !errors.Is(err, service.ErrActorRequired) {
		t.Fatalf("error = %v, want ErrActorRequired", err)
	}
	if len(repo.written) != 0 || len(repo.entries) != 0 {
		t.Fatal("an anonymous write must not reach the repository")
	}
}

// TestCreateAdminReportsATakenEmailAsConflict is what the panel's 409 is built on:
// the unique index answers, the service names it, and no second audit row appears.
func TestCreateAdminReportsATakenEmailAsConflict(t *testing.T) {
	repo := &fakeRepository{createErr: adminrepository.ErrDuplicateEmail}
	svc := newService(repo, service.BootstrapSettings{})

	_, err := svc.CreateAdmin(context.Background(), panelOwner(), &dto.CreateAdminRequest{
		Email:    "taken@example.test",
		Role:     "support",
		Password: "kata-sandi-yang-cukup-panjang",
	})
	if !errors.Is(err, service.ErrEmailTaken) {
		t.Fatalf("error = %v, want ErrEmailTaken", err)
	}
}

// TestCreateAdminPropagatesRepositoryFailures keeps a database blip from looking
// like a created admin.
func TestCreateAdminPropagatesRepositoryFailures(t *testing.T) {
	failure := errors.New("postgres is down")
	repo := &fakeRepository{createErr: failure}
	svc := newService(repo, service.BootstrapSettings{})

	_, err := svc.CreateAdmin(context.Background(), panelOwner(), &dto.CreateAdminRequest{
		Email:    "new@example.test",
		Role:     "support",
		Password: "kata-sandi-yang-cukup-panjang",
	})
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v, want the repository failure", err)
	}
}
