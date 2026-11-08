package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	adminhandler "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/handler"
	transactionsdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	transactionservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
)

// stubTransactions answers the panel's search; the buyer surfaces are not this
// handler's routes and exist only because transaction.Service is one interface.
type stubTransactions struct {
	page      *transactionsdto.AdminTransactionPage
	listErr   error
	lastQuery *transactionsdto.AdminTransactionFilter

	revealedPIN string
	revealedID  uint
	revealErr   error
}

func (s *stubTransactions) ListTransactions(
	_ context.Context,
	filter *transactionsdto.AdminTransactionFilter,
) (*transactionsdto.AdminTransactionPage, error) {
	s.lastQuery = filter
	return s.page, s.listErr
}

func (s *stubTransactions) Channels(
	context.Context,
	uint,
) ([]transactionsdto.ChannelResponse, error) {
	return nil, nil
}

func (s *stubTransactions) Create(
	context.Context,
	string,
	*transactionsdto.CreateTransactionRequest,
) (*transactionsdto.TransactionResponse, error) {
	return nil, nil
}

func (s *stubTransactions) Get(
	context.Context,
	string,
	uint,
) (*transactionsdto.TransactionResponse, error) {
	return nil, nil
}

func (s *stubTransactions) HandleCallback(context.Context, []byte, string) error { return nil }

func (s *stubTransactions) CurrentPIN(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (s *stubTransactions) CurrentAccount(
	context.Context,
	string,
) (*transactionservice.CurrentAccount, error) {
	return nil, nil
}

func (s *stubTransactions) History(
	context.Context,
	string,
	*transactionsdto.TransactionFilter,
) (*transactionsdto.TransactionPage, error) {
	return nil, nil
}

// RevealPIN is the panel's one write; the tests below drive it through the stub.
func (s *stubTransactions) RevealPIN(
	_ context.Context,
	_ audit.Actor,
	id uint,
) (string, error) {
	s.revealedID = id
	return s.revealedPIN, s.revealErr
}

// allowingLimiter stands in for the Redis counter: these tests are about the route,
// not the budget, and the budget has its own test in the middleware package.
type allowingLimiter struct{}

func (allowingLimiter) Allow(context.Context, string, int, time.Duration) (bool, error) {
	return true, nil
}

func transactionEngine(t *testing.T, transactions *stubTransactions, resolver stubResolver) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)
	guard := middleware.NewAdminGuard(resolver,
		middleware.AdminCookieSettings{Name: "hotspot_admin"}, zap.NewNop())

	revealLimit := middleware.NewPINRevealLimiter(allowingLimiter{},
		middleware.PINRevealLimitSettings{Window: time.Minute, Limit: 30}, zap.NewNop())

	engine := gin.New()
	adminhandler.NewTransactionHandler(transactions, guard, revealLimit).
		RegisterRoutes(engine.Group("/api"))
	return engine
}

func panelTransactionPage() *transactionsdto.AdminTransactionPage {
	lastSMS := "sent"
	return &transactionsdto.AdminTransactionPage{
		Items: []transactionsdto.AdminTransactionSummaryResponse{
			{
				ID: 12, Reference: "TRX-ONE", PhoneNumber: "6281234567890",
				PackageID: 1, PackageName: "Paket Harian", AmountIDR: 5000,
				Channel: "BRIVA", ChannelName: "BRIVA", State: "provisioned",
				GatewayReference: "GW-1", ProvisionAttempts: 1, LastSMSState: &lastSMS,
				CreatedAt: "2026-10-23T08:00:00Z", ExpiresAt: "2026-10-23T09:00:00Z",
			},
		},
		TotalCount: 1,
		Page:       1,
		PageSize:   20,
	}
}

// TestTransactionHandlerListsForEveryRoleEvenThoughOnlySomeMayRevealAPIN is the
// search's column of ADR 0024 §3: reading the list is every role's job, and the
// answer never carries a credential.
func TestTransactionHandlerListsForEveryRoleEvenThoughOnlySomeMayRevealAPIN(t *testing.T) {
	t.Parallel()

	for _, role := range []string{"owner", "support", "auditor"} {
		engine := transactionEngine(t, &stubTransactions{page: panelTransactionPage()},
			stubResolver{role: role})

		recorder := doPackageRequest(t, engine, http.MethodGet, "/api/admin/transactions", "", true)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s list = %d (%s)", role, recorder.Code, recorder.Body.String())
		}

		var body struct {
			Data       []map[string]any `json:"data"`
			TotalCount int64            `json:"total_count"`
			Page       int              `json:"page"`
			PageSize   int              `json:"page_size"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s list: %v", role, err)
		}
		if len(body.Data) != 1 || body.TotalCount != 1 || body.Page != 1 || body.PageSize != 20 {
			t.Fatalf("%s envelope = %+v", role, body)
		}
		if _, leaked := body.Data[0]["pin"]; leaked {
			t.Fatalf("%s response carries a pin field: %s", role, recorder.Body.String())
		}
	}
}

func TestTransactionHandlerRefusesAnUnauthenticatedCall(t *testing.T) {
	t.Parallel()

	engine := transactionEngine(t, &stubTransactions{page: panelTransactionPage()},
		stubResolver{role: "owner"})

	recorder := doPackageRequest(t, engine, http.MethodGet, "/api/admin/transactions", "", false)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d, want 401 (%s)", recorder.Code, recorder.Body.String())
	}
}

func TestTransactionHandlerMapsTheSearchRefusals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		listErr  error
		path     string
		wantCode int
		wantKey  string
	}{
		{
			name: "a filter the search cannot answer",
			listErr: &transactionservice.FilterError{
				Field: "state", Reason: "State transaksi tidak dikenal.",
			},
			path:     "/api/admin/transactions?state=shipped",
			wantCode: http.StatusUnprocessableEntity,
			wantKey:  "State transaksi tidak dikenal.",
		},
		{
			name:     "the sentinel without a field reason",
			listErr:  transactionservice.ErrFilterInvalid,
			path:     "/api/admin/transactions",
			wantCode: http.StatusUnprocessableEntity,
			wantKey:  "Filter transaksi tidak valid.",
		},
		{
			name:     "an unexpected failure stays generic",
			listErr:  errors.New("outbox exploded"),
			path:     "/api/admin/transactions",
			wantCode: http.StatusInternalServerError,
			wantKey:  "Terjadi kesalahan pada server.",
		},
		{
			name:     "a malformed page is the transport's own refusal",
			path:     "/api/admin/transactions?page=dua",
			wantCode: http.StatusBadRequest,
			wantKey:  "Filter tidak valid: page dan page_size harus angka dalam rentangnya.",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			engine := transactionEngine(t, &stubTransactions{
				page:    panelTransactionPage(),
				listErr: testCase.listErr,
			}, stubResolver{role: "owner"})

			recorder := doPackageRequest(t, engine, http.MethodGet, testCase.path, "", true)
			if recorder.Code != testCase.wantCode {
				t.Fatalf("%s = %d, want %d (%s)",
					testCase.path, recorder.Code, testCase.wantCode, recorder.Body.String())
			}

			if message, _ := errorPayload(t, recorder.Body.Bytes())["message"].(string); message != testCase.wantKey {
				t.Fatalf("message = %q, want %q", message, testCase.wantKey)
			}
		})
	}
}

func TestTransactionHandlerPassesTheFilterThroughUnchanged(t *testing.T) {
	t.Parallel()

	transactions := &stubTransactions{page: panelTransactionPage()}
	engine := transactionEngine(t, transactions, stubResolver{role: "owner"})

	recorder := doPackageRequest(t, engine, http.MethodGet,
		"/api/admin/transactions?state=paid&phone_number=08123456789&package_id=3"+
			"&from=2026-10-23&to=2026-10-24&page=2&page_size=50", "", true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list = %d (%s)", recorder.Code, recorder.Body.String())
	}

	if transactions.lastQuery == nil {
		t.Fatal("the handler never asked the service")
	}
	got := transactions.lastQuery
	if got.State != "paid" || got.PhoneNumber != "08123456789" || got.PackageID != 3 ||
		got.From != "2026-10-23" || got.To != "2026-10-24" || got.Page != 2 || got.PageSize != 50 {
		t.Fatalf("filter reached the service as %+v", got)
	}
}

// TestTransactionHandlerRevealsAPINForOwnerAndSupportOnly is the reveal's row of
// ADR 0024 §3 at the HTTP boundary: the person whose actions are most likely to be
// reviewed may not open a PIN, and no session gets one at all.
func TestTransactionHandlerRevealsAPINForOwnerAndSupportOnly(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		role     string
		cookie   bool
		wantCode int
	}{
		{role: "owner", cookie: true, wantCode: http.StatusOK},
		{role: "support", cookie: true, wantCode: http.StatusOK},
		{role: "auditor", cookie: true, wantCode: http.StatusForbidden},
		{role: "owner", cookie: false, wantCode: http.StatusUnauthorized},
	} {
		t.Run(testCase.role+"/cookie="+strconv.FormatBool(testCase.cookie), func(t *testing.T) {
			transactions := &stubTransactions{revealedPIN: "123456"}
			engine := transactionEngine(t, transactions, stubResolver{role: testCase.role})

			recorder := doPackageRequest(t, engine, http.MethodPost,
				"/api/admin/transactions/41/pin", "", testCase.cookie)
			if recorder.Code != testCase.wantCode {
				t.Fatalf("reveal as %s = %d, want %d (%s)",
					testCase.role, recorder.Code, testCase.wantCode, recorder.Body.String())
			}

			if testCase.wantCode != http.StatusOK {
				return
			}
			var body struct {
				Data struct {
					PIN string `json:"pin"`
				} `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode reveal: %v", err)
			}
			if body.Data.PIN != "123456" {
				t.Fatalf("pin = %q, want the service's value in the shared envelope", body.Data.PIN)
			}
			if transactions.revealedID != 41 {
				t.Fatalf("service was asked for Transaction %d, want 41", transactions.revealedID)
			}
		})
	}
}

// TestTransactionHandlerMapsPINRevealRefusals pins the status each refusal answers,
// including the generic 500 a key fault must stay behind (ADR 0016).
func TestTransactionHandlerMapsPINRevealRefusals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		path      string
		revealErr error
		wantCode  int
	}{
		{
			name:     "an id that is not a number is the transport's refusal",
			path:     "/api/admin/transactions/dua/pin",
			wantCode: http.StatusBadRequest,
		},
		{
			name:      "no such Transaction",
			path:      "/api/admin/transactions/999/pin",
			revealErr: transactionservice.ErrNotFound,
			wantCode:  http.StatusNotFound,
		},
		{
			name:      "not provisioned yet",
			path:      "/api/admin/transactions/41/pin",
			revealErr: transactionservice.ErrPINNotReady,
			wantCode:  http.StatusConflict,
		},
		{
			name:      "a key fault stays generic",
			path:      "/api/admin/transactions/41/pin",
			revealErr: transactionservice.ErrPINUnavailable,
			wantCode:  http.StatusInternalServerError,
		},
		{
			name:      "an unexpected failure stays generic",
			path:      "/api/admin/transactions/41/pin",
			revealErr: errors.New("database on fire"),
			wantCode:  http.StatusInternalServerError,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			engine := transactionEngine(t, &stubTransactions{revealErr: testCase.revealErr},
				stubResolver{role: "support"})

			recorder := doPackageRequest(t, engine, http.MethodPost, testCase.path, "", true)
			if recorder.Code != testCase.wantCode {
				t.Fatalf("%s = %d, want %d (%s)",
					testCase.path, recorder.Code, testCase.wantCode, recorder.Body.String())
			}

			payload := errorPayload(t, recorder.Body.Bytes())
			if testCase.wantCode == http.StatusInternalServerError {
				if payload["message"] != "Terjadi kesalahan pada server." {
					t.Fatalf("message = %v, want the generic sentence", payload["message"])
				}
			}
			if strings.Contains(recorder.Body.String(), "123456") {
				t.Fatalf("a refused reveal leaked a PIN: %s", recorder.Body.String())
			}
		})
	}
}
