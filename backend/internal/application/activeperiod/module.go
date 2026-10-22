// Package activeperiod is the public Active Period read slice (ADR 0005): the
// Login Page status page asks for the end of a Phone Number's Active Period and
// the answer comes from FreeRADIUS through the provisioning read port (ADR 0014).
package activeperiod

import (
	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/activeperiod/service"
)

// Module provides the Active Period service. It owns no entity or repository:
// the Active Period lives in FreeRADIUS, not in Postgres.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(service.NewService),
	)
}
