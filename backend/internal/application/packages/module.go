// Package packages is the Package catalog slice: entity, dto, repository,
// service and handler for the public Package list the Login Page reads.
//
// The Go package is named "packages" (plural) because "package" is a reserved
// keyword; the domain term, table and route stay Package/packages.
package packages

import (
	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
)

// Module provides the catalog repository and service. The HTTP handler is
// registered with the API transport, like every other domain route.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			repository.NewRepository,
			service.NewService,
		),
	)
}

// Migrations lists the Postgres tables this domain owns, for the deployment
// migration command (ADR 0012).
func Migrations() []any {
	return []any{&entity.Package{}}
}
