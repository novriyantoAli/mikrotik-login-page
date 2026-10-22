package api

import (
	"context"
	"errors"
	"fmt"
	stdlog "log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
)

// APIPrefix is the single path prefix the reverse proxy forwards to this
// service. It is part of the frontend contract (the axios client is bound to
// /api), so it lives here rather than in deployment configuration.
const APIPrefix = "/api"

// RouteRegistrar is implemented by every domain handler that owns routes. The
// server collects them through the api_routes Fx group, so adding an endpoint
// never means editing a central switch statement.
type RouteRegistrar interface {
	RegisterRoutes(api *gin.RouterGroup)
}

// NewEngine builds the bare Gin engine with the shared middleware stack.
func NewEngine(cfg *config.Config, logger *zap.Logger) (*gin.Engine, error) {
	if cfg.App.Environment != config.EnvironmentDevelopment {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()

	// Only the reverse proxy in front of us may speak for the client; with an
	// empty list gin trusts nobody and ClientIP() reports the socket peer.
	if err := engine.SetTrustedProxies(cfg.Server.TrustedProxyCIDRs()); err != nil {
		return nil, fmt.Errorf("set server.trusted_proxies: %w", err)
	}

	engine.Use(middleware.Recovery(logger), middleware.RequestLogger(logger), middleware.CORS())
	return engine, nil
}

// Router is the fully wired HTTP router. It is a distinct type so Fx can tell
// it apart from the bare engine built by NewEngine.
type Router struct {
	Engine *gin.Engine
}

// NewRoutingEngine mounts every registered domain under APIPrefix.
func NewRoutingEngine(engine *gin.Engine, registrars []RouteRegistrar) *Router {
	apiGroup := engine.Group(APIPrefix)
	for _, registrar := range registrars {
		registrar.RegisterRoutes(apiGroup)
	}
	return &Router{Engine: engine}
}

// NewHTTPServer binds the engine and shuts it down gracefully on stop.
func NewHTTPServer(
	lc fx.Lifecycle,
	cfg *config.Config,
	router *Router,
	shutdowner fx.Shutdowner,
	logger *zap.Logger,
) *http.Server {
	server := &http.Server{
		Addr:         net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port)),
		Handler:      router.Engine,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
		ErrorLog:     stdlog.New(zapLogWriter{logger: logger}, "", 0),
	}

	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			listener, err := net.Listen("tcp", server.Addr)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", server.Addr, err)
			}
			logger.Info("api listening", zap.String("addr", listener.Addr().String()))

			go func() {
				if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					// A listener that dies must not leave a half-dead process
					// that answers nothing while the orchestrator thinks it is up.
					logger.Error("http server stopped unexpectedly", zap.Error(err))
					if shutdownErr := shutdowner.Shutdown(fx.ExitCode(1)); shutdownErr != nil {
						logger.Error("request process shutdown", zap.Error(shutdownErr))
					}
				}
			}()
			return nil
		},
		OnStop: func(_ context.Context) error {
			logger.Info("shutting down api")

			shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
			defer cancel()

			return server.Shutdown(shutdownCtx)
		},
	})

	return server
}

// zapLogWriter routes net/http's own error log through the injected logger.
type zapLogWriter struct {
	logger *zap.Logger
}

func (w zapLogWriter) Write(message []byte) (int, error) {
	w.logger.Warn("http server error", zap.String("detail", strings.TrimSpace(string(message))))
	return len(message), nil
}
