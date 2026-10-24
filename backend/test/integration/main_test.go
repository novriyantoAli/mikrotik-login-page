package integration

import (
	"fmt"
	"os"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// TestMain starts a real Postgres and Redis for the whole package and tears them
// down afterwards. External servers can be supplied with TEST_POSTGRES_DSN,
// TEST_REDIS_ADDR and TEST_FREERADIUS_MYSQL_DSN (used by CI's service
// containers); Postgres and Redis fall back to real local instances when unset.
func TestMain(m *testing.M) {
	// CI provides the FreeRADIUS MySQL service, so a missing DSN there would make
	// the provisioning acceptance criteria a silent skip while the job stays green
	// (ADR 0016). A developer machine without MySQL keeps skipping.
	if os.Getenv("CI") != "" && os.Getenv("TEST_FREERADIUS_MYSQL_DSN") == "" {
		fmt.Fprintln(os.Stderr,
			"CI is set but TEST_FREERADIUS_MYSQL_DSN is empty: the FreeRADIUS write tests would skip")
		os.Exit(1)
	}

	os.Exit(testutil.RunWithDependencies(m))
}
