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

	authdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/handler"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
)

const testPhone = "6281200000001"

// stubService is the whole Transaction surface the handler uses, so these tests
// never build a repository, a catalog or a gateway.
type stubService struct {
	channels    []dto.ChannelResponse
	err         error
	transaction *dto.TransactionResponse
	heard       *dto.CreateTransactionRequest
	signature   string
}

func (s *stubService) Channels(context.Context) ([]dto.ChannelResponse, error) {
	return s.channels, s.err
}

func (s *stubService) Create(
	_ context.Context,
	_ string,
	request *dto.CreateTransactionRequest,
) (*dto.TransactionResponse, error) {
	s.heard = request
	return s.transaction, s.err
}

func (s *stubService) Get(context.Context, string, uint) (*dto.TransactionResponse, error) {
	return s.transaction, s.err
}

func (s *stubService) HandleCallback(_ context.Context, _ []byte, signature string) error {
	s.signature = signature
	return s.err
}

type stubAuthenticator struct{}

func (stubAuthenticator) Authenticate(
	_ context.Context,
	token string,
) (*authdto.SessionResponse, error) {
	if token != "good" {
		return nil, errors.New("no session")
	}
	return &authdto.SessionResponse{PhoneNumber: testPhone}, nil
}

func newEngine(transactions *stubService) *gin.Engine {
	gin.SetMode(gin.TestMode)

	guard := middleware.NewSessionGuard(stubAuthenticator{},
		middleware.SessionSettings{CookieName: "session"}, zap.NewNop())

	engine := gin.New()
	handler.NewHandler(transactions, guard).RegisterRoutes(engine.Group("/api"))
	return engine
}

func request(
	engine *gin.Engine,
	method, path string,
	body any,
	cookie string,
	header map[string]string,
) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}

	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(method, path, reader)
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		httpRequest.AddCookie(&http.Cookie{Name: "session", Value: cookie})
	}
	for key, value := range header {
		httpRequest.Header.Set(key, value)
	}

	engine.ServeHTTP(recorder, httpRequest)
	return recorder
}

func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
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

func TestChannels_RequiresASession(t *testing.T) {
	t.Parallel()

	recorder := request(newEngine(&stubService{}), http.MethodGet, "/api/transactions/channels", nil, "", nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestChannels_ListsTheGatewayChannels(t *testing.T) {
	t.Parallel()

	transactions := &stubService{channels: []dto.ChannelResponse{
		{Code: "BRIVA", Name: "BRIVA", Group: "Virtual Account", FeeFlat: 2500},
	}}
	recorder := request(newEngine(transactions), http.MethodGet, "/api/transactions/channels", nil, "good", nil)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"fee_flat_idr":2500`)) {
		t.Fatalf("fees must be separate integer rupiah: %s", recorder.Body.String())
	}
}

func TestCreate_AnotherPhoneNumberInTheBodyIsNotFound(t *testing.T) {
	t.Parallel()

	transactions := &stubService{transaction: &dto.TransactionResponse{ID: 1}}
	recorder := request(newEngine(transactions), http.MethodPost, "/api/transactions",
		map[string]any{"package_id": 7, "channel": "BRIVA", "phone_number": "6281299999999"},
		"good", nil)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if transactions.heard != nil {
		t.Fatal("the service must not be asked to create a Transaction for another Phone Number")
	}
}

func TestCreate_PassesTheSessionPhoneNumberToTheService(t *testing.T) {
	t.Parallel()

	transactions := &stubService{transaction: &dto.TransactionResponse{ID: 5, State: "pending"}}
	recorder := request(newEngine(transactions), http.MethodPost, "/api/transactions",
		map[string]any{"package_id": 7, "channel": "BRIVA"}, "good", nil)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if transactions.heard == nil || transactions.heard.PackageID != 7 {
		t.Fatalf("service heard %+v", transactions.heard)
	}
}

func TestCreate_UnknownChannelIsABadRequest(t *testing.T) {
	t.Parallel()

	transactions := &stubService{err: service.ErrChannelUnknown}
	recorder := request(newEngine(transactions), http.MethodPost, "/api/transactions",
		map[string]any{"package_id": 7, "channel": "KREDIT"}, "good", nil)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if code := errorCode(t, recorder); code != "invalid_request" {
		t.Fatalf("code = %q", code)
	}
}

func TestCreate_GatewayOutageIsADependencyFailure(t *testing.T) {
	t.Parallel()

	transactions := &stubService{err: service.ErrGatewayUnavailable}
	recorder := request(newEngine(transactions), http.MethodPost, "/api/transactions",
		map[string]any{"package_id": 7, "channel": "BRIVA"}, "good", nil)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", recorder.Code)
	}
}

func TestGet_UnknownTransactionIsNotFound(t *testing.T) {
	t.Parallel()

	transactions := &stubService{err: service.ErrNotFound}
	recorder := request(newEngine(transactions), http.MethodGet, "/api/transactions/99", nil, "good", nil)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestCallback_IsPublicButRejectsAWrongSignature(t *testing.T) {
	t.Parallel()

	transactions := &stubService{err: payment.ErrSignatureMismatch}
	engine := newEngine(transactions)
	recorder := request(engine, http.MethodPost, "/api/payments/callback",
		map[string]any{"success": true}, "", map[string]string{"X-Callback-Signature": "forged"})

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if transactions.signature != "forged" {
		t.Fatalf("handler forwarded signature %q", transactions.signature)
	}
}

func TestCallback_AcceptsAVerifiedNotification(t *testing.T) {
	t.Parallel()

	recorder := request(newEngine(&stubService{}), http.MethodPost, "/api/payments/callback",
		map[string]any{"success": true}, "", map[string]string{"X-Callback-Signature": "good"})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"received":true`)) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}
