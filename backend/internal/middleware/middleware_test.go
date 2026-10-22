package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
)

func serve(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.CORS())
	engine.GET("/api/packages", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"data": []any{}}) })
	engine.OPTIONS("/api/packages", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"data": []any{}}) })
	engine.POST("/api/anything", func(c *gin.Context) { c.Status(http.StatusCreated) })

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func TestCORS_LetsTheLoginPageReadPublicEndpoints(t *testing.T) {
	t.Parallel()

	recorder := serve(t, http.MethodGet, "/api/packages")
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("credentials must never be allowed, got %q", got)
	}
}

func TestCORS_AnswersPreflightForReadsOnly(t *testing.T) {
	t.Parallel()

	recorder := serve(t, http.MethodOptions, "/api/packages")
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Methods"); got != "GET, HEAD, OPTIONS" {
		t.Fatalf("Access-Control-Allow-Methods = %q", got)
	}
}

func TestCORS_DoesNotOpenStateChangingMethods(t *testing.T) {
	t.Parallel()

	recorder := serve(t, http.MethodPost, "/api/anything")
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("a POST answered with Access-Control-Allow-Origin %q", got)
	}
}
