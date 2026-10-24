package provisioning_test

import (
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
)

func TestTarget_ExtendsAnActivePeriodInsteadOfRestartingIt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 1, 8, 0, 0, 0, time.UTC)
	current := provisioning.Expiration{ExpiresAt: now.Add(12 * time.Hour), Found: true}

	got := provisioning.Target(current, now, now, 24*time.Hour)
	want := current.ExpiresAt.Add(24 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("Target = %s, want the running Active Period extended to %s", got, want)
	}
}

func TestTarget_StartsFromTheBaseWhenNothingIsRunning(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 1, 8, 0, 0, 0, time.UTC)
	settledAt := now.Add(10 * time.Minute)

	for name, current := range map[string]provisioning.Expiration{
		"absent":  {},
		"expired": {ExpiresAt: now.Add(-time.Minute), Found: true},
		"ending now": {
			ExpiresAt: now, Found: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := provisioning.Target(current, now, settledAt, 24*time.Hour)
			want := settledAt.Add(24 * time.Hour)
			if !got.Equal(want) {
				t.Fatalf("Target = %s, want the base plus duration (%s)", got, want)
			}
		})
	}
}
