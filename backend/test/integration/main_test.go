package integration

import (
	"os"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// TestMain starts a real Postgres and Redis for the whole package and tears them
// down afterwards. External servers can be supplied with TEST_POSTGRES_DSN,
// TEST_REDIS_ADDR and TEST_FREERADIUS_MYSQL_DSN (used by CI's service
// containers); Postgres and Redis fall back to real local instances when unset.
func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithDependencies(m))
}
