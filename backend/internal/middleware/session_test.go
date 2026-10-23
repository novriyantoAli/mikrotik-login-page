package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	authdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/dto"
	authservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
)

// fakeAuthenticator is the two-method edge the middleware needs, not the whole
// auth domain: the test never builds a repository or a clock.
type fakeAuthenticator struct {
	session *authdto.SessionResponse
	err     error
}

func (f fakeAuthenticator) Authenticate(
	_ context.Context,
	sessionToken string,
) (*authdto.SessionResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.session == nil {
		return nil, authservice.ErrUnauthenticated
	}
	return f.session, nil
}

func newRouter(auth middleware.SessionAuthenticator) *gin.Engine {
	gin.SetMode(gin.TestMode)

	guard := middleware.NewSessionGuard(auth,
		middleware.SessionSettings{CookieName: "session", Secure: true},
		zap.NewNop(),
	)

	engine := gin.New()
	protected := engine.Group("/api", guard.Require())
	protected.GET("/me", func(c *gin.Context) {
		session, ok := middleware.SessionFrom(c)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no session in context"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"phone_number": session.PhoneNumber}})
	})
	protected.GET("/phone/:phone", func(c *gin.Context) {
		if !middleware.RequireOwnPhoneNumber(c, c.Param("phone")) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"phone_number": c.Param("phone")}})
	})
	protected.GET("/phone-empty", func(c *gin.Context) {
		if !middleware.RequireOwnPhoneNumber(c, "") {
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"phone_number": ""}})
	})
	return engine
}

func do(engine *gin.Engine, path string, sessionCookie string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if sessionCookie != "" {
		request.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func decodeErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return body.Error.Code
}

func TestRequire_NoCookieIsUnauthorized(t *testing.T) {
	t.Parallel()

	recorder := do(newRouter(fakeAuthenticator{}), "/api/me", "")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if code := decodeErrorCode(t, recorder); code != "unauthorized" {
		t.Fatalf("error code = %q, want unauthorized", code)
	}
}

func TestRequire_StaleCookieIsClearedAndUnauthorized(t *testing.T) {
	t.Parallel()

	recorder := do(newRouter(fakeAuthenticator{err: authservice.ErrUnauthenticated}), "/api/me", "stale")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	header := recorder.Header().Get("Set-Cookie")
	if header == "" {
		t.Fatal("an unusable session cookie must be cleared")
	}
	if !bytes.Contains([]byte(header), []byte("Max-Age=0")) {
		t.Fatalf("cookie was not expired: %s", header)
	}
}

func TestRequire_DatabaseFailureIsInternalNotUnauthorized(t *testing.T) {
	t.Parallel()

	recorder := do(newRouter(fakeAuthenticator{err: errors.New("postgres is down")}), "/api/me", "token")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if code := decodeErrorCode(t, recorder); code != "internal_error" {
		t.Fatalf("error code = %q, want internal_error", code)
	}
}

func TestRequire_ValidSessionReachesTheHandler(t *testing.T) {
	t.Parallel()

	auth := fakeAuthenticator{session: &authdto.SessionResponse{PhoneNumber: "6281200000001"}}
	recorder := do(newRouter(auth), "/api/me", "good")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("6281200000001")) {
		t.Fatalf("handler did not see the session: %s", recorder.Body.String())
	}
}

func TestRequireOwnPhoneNumber_AnotherNumberIsNotFound(t *testing.T) {
	t.Parallel()

	auth := fakeAuthenticator{session: &authdto.SessionResponse{PhoneNumber: "6281200000001"}}
	recorder := do(newRouter(auth), "/api/phone/6281299999999", "good")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (never 403: the other buyer's data must not be confirmable)",
			recorder.Code)
	}
}

func TestRequireOwnPhoneNumber_OwnNumberIsAllowed(t *testing.T) {
	t.Parallel()

	auth := fakeAuthenticator{session: &authdto.SessionResponse{PhoneNumber: "6281200000001"}}
	recorder := do(newRouter(auth), "/api/phone/6281200000001", "good")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestRequireOwnPhoneNumber_EmptyNumberIsAllowed(t *testing.T) {
	t.Parallel()

	auth := fakeAuthenticator{session: &authdto.SessionResponse{PhoneNumber: "6281200000001"}}
	recorder := do(newRouter(auth), "/api/phone-empty", "good")

	// An empty path value is not a mismatch: the endpoint simply did not name a
	// Phone Number, and the session already scopes it.
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}
