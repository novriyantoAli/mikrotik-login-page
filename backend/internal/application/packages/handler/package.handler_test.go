package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/handler"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
)

type stubCatalog struct {
	packages []dto.PackageResponse
	err      error
}

func (s stubCatalog) ListPackages(context.Context) ([]dto.PackageResponse, error) {
	return s.packages, s.err
}

func (stubCatalog) GetPackage(context.Context, uint) (*entity.Package, error) {
	return nil, service.ErrPackageNotFound
}

func (stubCatalog) Seed(context.Context) (int64, error) { return 0, nil }

func getPackages(t *testing.T, catalog stubCatalog) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler.NewHandler(catalog).RegisterRoutes(engine.Group("/api"))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/packages", nil))
	return recorder
}

func TestGetPackages_WritesTheFrozenEnvelope(t *testing.T) {
	t.Parallel()

	recorder := getPackages(t, stubCatalog{packages: []dto.PackageResponse{
		{ID: 1, Name: "Paket Harian", Price: "Rp 5.000", ActivePeriod: "1 Hari", SpeedLimit: "5M"},
	}})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("data length = %d, want 1", len(body.Data))
	}

	want := map[string]bool{"id": true, "nama": true, "harga": true, "durasiActivePeriod": true, "batasKecepatan": true}
	for key := range body.Data[0] {
		if !want[key] {
			t.Fatalf("unexpected field %q in the frozen contract", key)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing contract fields: %v", want)
	}
}

func TestGetPackages_EmptyCatalogIsAnEmptyArray(t *testing.T) {
	t.Parallel()

	recorder := getPackages(t, stubCatalog{})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); body != `{"data":[]}` {
		t.Fatalf("body = %s, want {\"data\":[]}", body)
	}
}

func TestGetPackages_FailureUsesTheStableErrorEnvelope(t *testing.T) {
	t.Parallel()

	recorder := getPackages(t, stubCatalog{err: errors.New("postgres is down")})

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if body.Error.Code == "" || body.Error.Message == "" {
		t.Fatalf("error envelope is incomplete: %s", recorder.Body.String())
	}
	if body.Error.Message == "postgres is down" {
		t.Fatal("the internal error text leaked to the client")
	}
}

// The stub must satisfy the domain interface; a compile-time assertion keeps it
// honest if the interface grows.
var _ service.Service = stubCatalog{}
