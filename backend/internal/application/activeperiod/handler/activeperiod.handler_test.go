package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/activeperiod/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/activeperiod/handler"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/activeperiod/service"
)

type stubActivePeriod struct {
	response *dto.ActivePeriodResponse
	err      error
}

func (s stubActivePeriod) GetActivePeriod(context.Context, string) (*dto.ActivePeriodResponse, error) {
	return s.response, s.err
}

func getActivePeriod(t *testing.T, stub stubActivePeriod, target string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler.NewHandler(stub).RegisterRoutes(engine.Group("/api"))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func TestGetActivePeriod_WritesTheExpiryEnvelope(t *testing.T) {
	t.Parallel()

	expiry := "2026-01-10T12:00:00Z"
	recorder := getActivePeriod(t,
		stubActivePeriod{response: &dto.ActivePeriodResponse{Expiry: &expiry}},
		"/api/active-period?phone=08123456789",
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); body != `{"data":{"expiry":"2026-01-10T12:00:00Z"}}` {
		t.Fatalf("body = %s", body)
	}
}

func TestGetActivePeriod_UnknownNumberIsNullExpiry(t *testing.T) {
	t.Parallel()

	recorder := getActivePeriod(t, stubActivePeriod{response: &dto.ActivePeriodResponse{}},
		"/api/active-period?phone=628123456789")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); body != `{"data":{"expiry":null}}` {
		t.Fatalf("body = %s", body)
	}
}

func TestGetActivePeriod_MalformedNumberIsAClientError(t *testing.T) {
	t.Parallel()

	recorder := getActivePeriod(t,
		stubActivePeriod{err: service.ErrInvalidPhoneNumber},
		"/api/active-period?phone=nope",
	)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "invalid_request")
}

func TestGetActivePeriod_DependencyFailureIs503WithAGenericMessage(t *testing.T) {
	t.Parallel()

	recorder := getActivePeriod(t,
		stubActivePeriod{err: errors.New("dial tcp 10.0.0.5:3306: connection refused")},
		"/api/active-period?phone=628123456789",
	)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	assertErrorCode(t, recorder, "dependency_unavailable")
	if body := recorder.Body.String(); strings.Contains(body, "10.0.0.5") || strings.Contains(body, "connection refused") {
		t.Fatalf("driver detail leaked to the client: %s", body)
	}
}

func assertErrorCode(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if body.Error.Code != want {
		t.Fatalf("error code = %q, want %q", body.Error.Code, want)
	}
	if body.Error.Message == "" {
		t.Fatal("error message is empty")
	}
}

var _ service.Service = stubActivePeriod{}
