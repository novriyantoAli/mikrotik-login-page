package handler_test

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

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/account/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/account/handler"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/account/service"
	authdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
)

const accountTestPhone = "6281200000007"

// stubAccountService is the whole Account Status surface the handler uses, so
// these tests never build a database, a catalog or the provisioning port.
type stubAccountService struct {
	summary *dto.AccountResponse
	preview *dto.ExtendPreviewResponse
	err     error

	summaryPhone   string
	previewPhone   string
	previewPackage uint
	summaryCalls   int
}

func (s *stubAccountService) Summary(
	_ context.Context,
	phoneNumber string,
) (*dto.AccountResponse, error) {
	s.summaryCalls++
	s.summaryPhone = phoneNumber
	return s.summary, s.err
}

func (s *stubAccountService) ExtendPreview(
	_ context.Context,
	phoneNumber string,
	packageID uint,
) (*dto.ExtendPreviewResponse, error) {
	s.previewPhone = phoneNumber
	s.previewPackage = packageID
	return s.preview, s.err
}

type stubAccountAuthenticator struct{}

func (stubAccountAuthenticator) Authenticate(
	_ context.Context,
	token string,
) (*authdto.SessionResponse, error) {
	if token != "good" {
		return nil, errors.New("no session")
	}
	return &authdto.SessionResponse{PhoneNumber: accountTestPhone}, nil
}

func newAccountEngine(accounts *stubAccountService) *gin.Engine {
	gin.SetMode(gin.TestMode)

	guard := middleware.NewSessionGuard(stubAccountAuthenticator{},
		middleware.SessionSettings{CookieName: "session"}, zap.NewNop())

	engine := gin.New()
	handler.NewHandler(accounts, guard).RegisterRoutes(engine.Group("/api"))
	return engine
}

func accountRequest(
	engine *gin.Engine,
	path, cookie string,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, bytes.NewReader(nil))
	if cookie != "" {
		request.AddCookie(&http.Cookie{Name: "session", Value: cookie})
	}
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestAccount_RequiresASession(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/api/account", "/api/account/extend-preview?package_id=1"} {
		recorder := accountRequest(newAccountEngine(&stubAccountService{}), path, "")
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s without a session = %d, want 401", path, recorder.Code)
		}
	}
}

func TestAccount_SummaryUsesTheSessionPhoneNumber(t *testing.T) {
	t.Parallel()

	pinValue := "123456"
	accounts := &stubAccountService{summary: &dto.AccountResponse{
		PhoneNumber: accountTestPhone,
		ActivePeriod: dto.ActivePeriodResponse{
			Found: true, State: dto.StateActive,
		},
		PIN: &pinValue,
	}}
	recorder := accountRequest(newAccountEngine(accounts), "/api/account", "good")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if accounts.summaryPhone != accountTestPhone {
		t.Fatalf("service heard %q, want the session's Phone Number", accounts.summaryPhone)
	}
	for _, expected := range []string{`"state":"active"`, `"pin":"123456"`, `"active_period"`} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(expected)) {
			t.Fatalf("body %s does not contain %s", recorder.Body.String(), expected)
		}
	}
}

func TestAccount_AnotherPhoneNumberInTheQueryIsNotFound(t *testing.T) {
	t.Parallel()

	accounts := &stubAccountService{summary: &dto.AccountResponse{}}
	recorder := accountRequest(newAccountEngine(accounts),
		"/api/account?phone_number=6281299999999", "good")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (never 403)", recorder.Code)
	}
	if accounts.summaryCalls != 0 {
		t.Fatal("the service must not be asked for another Phone Number's account")
	}
}

func TestAccount_ExtendPreviewPassesTheSelectedPackage(t *testing.T) {
	t.Parallel()

	accounts := &stubAccountService{preview: &dto.ExtendPreviewResponse{
		PackageID: 9, EstimatedExpiration: "2026-03-02T08:30:00Z", Extends: true,
	}}
	recorder := accountRequest(newAccountEngine(accounts),
		"/api/account/extend-preview?package_id=9", "good")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if accounts.previewPhone != accountTestPhone || accounts.previewPackage != 9 {
		t.Fatalf("service heard (%q, %d)", accounts.previewPhone, accounts.previewPackage)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"estimated_expiration":"2026-03-02T08:30:00Z"`)) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestAccount_ExtendPreviewRequiresAPackage(t *testing.T) {
	t.Parallel()

	recorder := accountRequest(newAccountEngine(&stubAccountService{}),
		"/api/account/extend-preview", "good")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestAccount_ExtendPreviewOfAnUnknownPackageIsABadRequest(t *testing.T) {
	t.Parallel()

	accounts := &stubAccountService{err: service.ErrPackageUnknown}
	recorder := accountRequest(newAccountEngine(accounts),
		"/api/account/extend-preview?package_id=99", "good")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	if body.Error.Code != "invalid_request" {
		t.Fatalf("code = %q", body.Error.Code)
	}
}
