package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	admindto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	adminhandler "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/handler"
	packagesdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	packagesentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	packageservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

const testAdminCookie = "admin-token"

// stubResolver is the one method the guard needs; it answers an owner unless a
// test asks for another role or no session.
type stubResolver struct {
	role string
	err  error
}

func (r stubResolver) Session(context.Context, string) (*admindto.SessionResponse, error) {
	if r.err != nil {
		return nil, r.err
	}
	return &admindto.SessionResponse{
		AdminID:   1,
		Email:     "owner@example.test",
		Role:      r.role,
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

// stubCatalog answers the four panel calls; a test names only what it exercises.
type stubCatalog struct {
	adminPackages []packagesdto.AdminPackageResponse
	listErr       error

	created   *packagesdto.AdminPackageResponse
	createErr error

	updated   *packagesdto.AdminPackageResponse
	updateErr error

	activated *packagesdto.AdminPackageResponse
	activeErr error
}

func (c stubCatalog) ListPackages(context.Context) ([]packagesdto.PackageResponse, error) {
	return nil, nil
}

func (c stubCatalog) GetPackage(context.Context, uint) (*packagesentity.Package, error) {
	return nil, packageservice.ErrPackageNotFound
}

func (c stubCatalog) Seed(context.Context) (int64, error) { return 0, nil }

func (c stubCatalog) ListAdminPackages(context.Context) ([]packagesdto.AdminPackageResponse, error) {
	return c.adminPackages, c.listErr
}

func (c stubCatalog) CreatePackage(
	context.Context,
	audit.Actor,
	*packagesdto.CreatePackageRequest,
) (*packagesdto.AdminPackageResponse, error) {
	return c.created, c.createErr
}

func (c stubCatalog) UpdatePackage(
	context.Context,
	audit.Actor,
	uint,
	*packagesdto.UpdatePackageRequest,
) (*packagesdto.AdminPackageResponse, error) {
	return c.updated, c.updateErr
}

func (c stubCatalog) SetActive(
	context.Context,
	audit.Actor,
	uint,
	*packagesdto.SetActiveRequest,
) (*packagesdto.AdminPackageResponse, error) {
	return c.activated, c.activeErr
}

func packageEngine(t *testing.T, catalog stubCatalog, resolver stubResolver) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)
	guard := middleware.NewAdminGuard(resolver,
		middleware.AdminCookieSettings{Name: "hotspot_admin"}, zap.NewNop())

	engine := gin.New()
	adminhandler.NewPackageHandler(catalog, guard).RegisterRoutes(engine.Group("/api"))
	return engine
}

func doPackageRequest(
	t *testing.T,
	engine *gin.Engine,
	method, path, body string,
	withCookie bool,
) *httptest.ResponseRecorder {
	t.Helper()

	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	if withCookie {
		request.AddCookie(&http.Cookie{Name: "hotspot_admin", Value: testAdminCookie})
	}

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func validPackage() packagesdto.AdminPackageResponse {
	return packagesdto.AdminPackageResponse{
		ID: 7, Code: "harian", Name: "Paket Harian", PriceIDR: 5000,
		DurationMinutes: 1440, RateLimit: "5M/5M", IsActive: true, Version: 2,
	}
}

func errorPayload(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var decoded struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return decoded.Error
}

func TestPackageHandlerListsTheWholeCatalogForEveryRole(t *testing.T) {
	t.Parallel()

	for _, role := range []string{"owner", "support", "auditor"} {
		engine := packageEngine(t, stubCatalog{adminPackages: []packagesdto.AdminPackageResponse{validPackage()}},
			stubResolver{role: role})

		recorder := doPackageRequest(t, engine, http.MethodGet, "/api/admin/packages", "", true)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s list = %d (%s)", role, recorder.Code, recorder.Body.String())
		}

		var envelope struct {
			Data []packagesdto.AdminPackageResponse `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(envelope.Data) != 1 || envelope.Data[0].Version != 2 {
			t.Fatalf("%s list = %+v, want the row with its version", role, envelope.Data)
		}
	}
}

func TestPackageHandlerWritesAreOwnerOnly(t *testing.T) {
	t.Parallel()

	engine := packageEngine(t, stubCatalog{}, stubResolver{role: "support"})
	requests := []struct{ method, path, body string }{
		{http.MethodPost, "/api/admin/packages", `{"code":"x"}`},
		{http.MethodPut, "/api/admin/packages/7", `{"version":2}`},
		{http.MethodPost, "/api/admin/packages/7/active", `{"active":false}`},
	}
	for _, request := range requests {
		recorder := doPackageRequest(t, engine, request.method, request.path, request.body, true)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s %s as support = %d, want 403", request.method, request.path, recorder.Code)
		}
		if code := errorPayload(t, recorder.Body.Bytes())["code"]; code != httpx.CodeForbidden {
			t.Fatalf("error code = %v, want %q", code, httpx.CodeForbidden)
		}
	}
}

func TestPackageHandlerRejectsARequestWithoutASession(t *testing.T) {
	t.Parallel()

	engine := packageEngine(t, stubCatalog{}, stubResolver{role: "owner"})
	if recorder := doPackageRequest(t, engine, http.MethodGet, "/api/admin/packages", "", false); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("list without a cookie = %d, want 401", recorder.Code)
	}
}

func TestPackageHandlerWrites201OnCreate(t *testing.T) {
	t.Parallel()

	created := validPackage()
	engine := packageEngine(t, stubCatalog{created: &created}, stubResolver{role: "owner"})

	recorder := doPackageRequest(t, engine, http.MethodPost, "/api/admin/packages",
		`{"code":"harian","name":"Paket Harian","price_idr":5000,"duration_minutes":1440,"rate_limit":"5M/5M"}`, true)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create = %d (%s)", recorder.Code, recorder.Body.String())
	}
}

func TestPackageHandlerMapsARefusedWrite(t *testing.T) {
	t.Parallel()

	const validBody = `{"name":"Paket Harian","price_idr":5000,"duration_minutes":1440,"rate_limit":"5M/5M","version":2}`
	cases := []struct {
		name    string
		catalog stubCatalog
		method  string
		path    string
		body    string
		want    int
		code    string
	}{
		{
			name:    "duplicate code",
			catalog: stubCatalog{createErr: packageservice.ErrPackageCodeTaken},
			method:  http.MethodPost, path: "/api/admin/packages",
			body: `{"code":"harian","name":"n","price_idr":1,"duration_minutes":1,"rate_limit":"1M/1M"}`,
			want: http.StatusConflict, code: httpx.CodeConflict,
		},
		{
			name:    "field rule",
			catalog: stubCatalog{createErr: &packageservice.FieldError{Field: "price_idr", Reason: "Harga salah."}},
			method:  http.MethodPost, path: "/api/admin/packages",
			body: `{"code":"harian","name":"n","price_idr":1,"duration_minutes":1,"rate_limit":"1M/1M"}`,
			want: http.StatusUnprocessableEntity, code: httpx.CodeInvalidRequest,
		},
		{
			name:    "immutable code",
			catalog: stubCatalog{updateErr: packageservice.ErrPackageCodeImmutable},
			method:  http.MethodPut, path: "/api/admin/packages/7", body: validBody,
			want: http.StatusUnprocessableEntity, code: httpx.CodeInvalidRequest,
		},
		{
			name:    "not found",
			catalog: stubCatalog{updateErr: packageservice.ErrPackageNotFound},
			method:  http.MethodPut, path: "/api/admin/packages/7", body: validBody,
			want: http.StatusNotFound, code: httpx.CodeNotFound,
		},
		{
			name:    "last active package",
			catalog: stubCatalog{activeErr: packageservice.ErrLastActivePackage},
			method:  http.MethodPost, path: "/api/admin/packages/7/active", body: `{"active":false}`,
			want: http.StatusConflict, code: httpx.CodeConflict,
		},
		{
			name:    "non-numeric id",
			catalog: stubCatalog{},
			method:  http.MethodPut, path: "/api/admin/packages/not-a-number", body: validBody,
			want: http.StatusNotFound, code: httpx.CodeNotFound,
		},
		{
			name:    "malformed body",
			catalog: stubCatalog{},
			method:  http.MethodPost, path: "/api/admin/packages", body: `{`,
			want: http.StatusBadRequest, code: httpx.CodeInvalidRequest,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := packageEngine(t, testCase.catalog, stubResolver{role: "owner"})
			recorder := doPackageRequest(t, engine, testCase.method, testCase.path, testCase.body, true)
			if recorder.Code != testCase.want {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, testCase.want, recorder.Body.String())
			}
			if code := errorPayload(t, recorder.Body.Bytes())["code"]; code != testCase.code {
				t.Fatalf("error code = %v, want %q", code, testCase.code)
			}
		})
	}
}

func TestPackageHandlerCarriesTheCurrentRowOnAStaleVersion(t *testing.T) {
	t.Parallel()

	conflict := &packageservice.VersionConflictError{Current: validPackage()}
	engine := packageEngine(t, stubCatalog{updateErr: conflict}, stubResolver{role: "owner"})

	recorder := doPackageRequest(t, engine, http.MethodPut, "/api/admin/packages/7",
		`{"name":"Paket Harian","price_idr":5000,"duration_minutes":1440,"rate_limit":"5M/5M","version":1}`, true)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", recorder.Code, recorder.Body.String())
	}

	var decoded packagesdto.VersionConflictResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if decoded.Error.Current.Version != 2 || decoded.Error.Current.ID != 7 {
		t.Fatalf("conflict body = %+v, want the current row", decoded.Error.Current)
	}
}

// The stub must satisfy the domain interface; a compile-time assertion keeps it
// honest if the interface grows.
var _ packageservice.Service = stubCatalog{}
