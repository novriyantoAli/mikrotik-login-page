// Package worker wires the background-job process: Asynq server, Redis client
// and the task mux. It shares its image with the API but not its command.
package worker

import (
	"github.com/hibiken/asynq"
	"go.uber.org/fx"
)

const tasksGroup = `group:"worker_tasks"`

// Module provides the worker server and collects task handlers.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			fx.Annotate(NewServeMux, fx.ParamTags(tasksGroup)),
			NewServer,
		),
		// The invoke forces construction: NewServer appends the lifecycle hooks
		// that start and stop processing.
		fx.Invoke(func(*asynq.Server) {}),
	)
}
