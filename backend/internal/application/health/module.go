// Package health is the dependency-health slice: DTO, service, handler and Fx
// wiring. It has no entity or repository — it never persists anything, it only
// probes what other packages own.
package health

import (
	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/service"
)

// Module provides the health service. Checkers arrive through the
// health_checkers Fx group, which infrastructure packages contribute to.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			fx.Annotate(
				service.NewService,
				fx.ParamTags(`group:"`+service.CheckersGroup+`"`),
			),
		),
	)
}
