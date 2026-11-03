package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
)

type fakeRepository struct {
	admins    int64
	countErr  error
	inserted  int64
	insertErr error
	created   []entity.Admin
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
