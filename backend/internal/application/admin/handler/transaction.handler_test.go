package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	adminhandler "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/handler"
	transactionsdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	transactionservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
)

// stubTransactions answers the panel's search; the buyer surfaces are not this
// handler's routes and exist only because transaction.Service is one interface.
type stubTransactions struct {
	page      *transactionsdto.AdminTransactionPage
	listErr   error
	lastQuery *transactionsdto.AdminTransactionFilter
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

func transactionEngine(t *testing.T, transactions *stubTransactions, resolver stubResolver) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)
	guard := middleware.NewAdminGuard(resolver,
		middleware.AdminCookieSettings{Name: "hotspot_admin"}, zap.NewNop())

	engine := gin.New()
	adminhandler.NewTransactionHandler(transactions, guard).RegisterRoutes(engine.Group("/api"))
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
