package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

type channelsEnvelope struct {
	Data []dto.ChannelResponse `json:"data"`
}

type transactionEnvelope struct {
	Data dto.TransactionResponse `json:"data"`
}

// TestChannelsContract_FixtureMatchesDTO proves the channel DTO models exactly
// the shared fixture: unknown fields fail decoding and a forgotten field fails
// the round-trip shape comparison. The app parses the same file with a strict
// zod schema.
func TestChannelsContract_FixtureMatchesDTO(t *testing.T) {
	raw := loadContract(t, "channels.response.json")

	var envelope channelsEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("fixture does not decode into the channels DTO: %v", err)
	}
	if len(envelope.Data) == 0 {
		t.Fatal("fixture must contain at least one channel")
	}

	roundTrip, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal channels DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, raw), decodeJSON(t, roundTrip), "$")
}

func TestChannelsContract_LiveResponseMatchesFixture(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081200000031", "198.51.100.31")

	recorder := harness.DoRequest(t, http.MethodGet, "/api/transactions/channels", nil,
		withCookie(cookie, "198.51.100.131"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", recorder.Code, recorder.Body.String())
	}

	fixture := decodeJSON(t, loadContract(t, "channels.response.json")).(map[string]any)
	fixtureEntry := fixture["data"].([]any)[0].(map[string]any)

	var live map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &live); err != nil {
		t.Fatalf("decode live channels: %v", err)
	}
	for _, entry := range live["data"].([]any) {
		assertSameKeys(t, fixtureEntry, entry.(map[string]any), "$.data[]")
	}
}

func TestTransactionContract_RequestFixtureMatchesDTO(t *testing.T) {
	raw := loadContract(t, "transaction.create.request.json")

	var request dto.CreateTransactionRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		t.Fatalf("fixture does not decode into dto.CreateTransactionRequest: %v", err)
	}
	if request.PackageID == 0 || request.Channel == "" {
		t.Fatalf("fixture request is incomplete: %+v", request)
	}
}

func TestTransactionContract_ResponseFixturesMatchDTO(t *testing.T) {
	for _, name := range []string{"transaction.response.json", "transaction.paid.response.json"} {
		raw := loadContract(t, name)

		var envelope transactionEnvelope
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&envelope); err != nil {
			t.Fatalf("%s does not decode into the transaction DTO: %v", name, err)
		}
		if envelope.Data.Reference == "" || envelope.Data.AmountIDR == 0 {
			t.Fatalf("%s is incomplete: %+v", name, envelope.Data)
		}

		roundTrip, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		assertSameJSONShape(t, decodeJSON(t, raw), decodeJSON(t, roundTrip), "$")
	}
}

// TestTransactionContract_LiveResponsesMatchFixtures walks one real checkout
// through settlement and compares both states with the shared fixtures, so a DTO
// field the app does not know about (or a fixture the DTO forgets) fails here.
func TestTransactionContract_LiveResponsesMatchFixtures(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081200000032", "198.51.100.32")
	packageID := firstPackageID(t, harness)

	recorder := harness.DoRequest(t, http.MethodPost, "/api/transactions",
		mustJSON(t, map[string]any{"package_id": packageID, "channel": "BRIVA"}),
		withCookie(cookie, "198.51.100.132"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("checkout status = %d (%s)", recorder.Code, recorder.Body.String())
	}
	pendingFixture := decodeJSON(t, loadContract(t, "transaction.response.json"))
	assertSameJSONShape(t, pendingFixture, decodeJSON(t, recorder.Body.Bytes()), "$")

	created := decodeTransaction(t, recorder.Body.Bytes())
	if code := postCallback(t, harness, created.Reference, payment.StatusPaid, created.AmountIDR,
		testutil.ValidCallbackSignature); code != http.StatusOK {
		t.Fatalf("settle status = %d", code)
	}

	paidRecorder := harness.DoRequest(t, http.MethodGet, "/api/transactions/"+uintString(created.ID), nil,
		withCookie(cookie, "198.51.100.132"))
	if paidRecorder.Code != http.StatusOK {
		t.Fatalf("status read = %d (%s)", paidRecorder.Code, paidRecorder.Body.String())
	}
	paidFixture := decodeJSON(t, loadContract(t, "transaction.paid.response.json"))
	assertSameJSONShape(t, paidFixture, decodeJSON(t, paidRecorder.Body.Bytes()), "$")
}

// TestTransactionCallbackContract_AckShape pins the public callback answer: it
// must not grow a field that leaks whether the reference exists.
func TestTransactionCallbackContract_AckShape(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	recorder := harness.DoRequest(t, http.MethodPost, "/api/payments/callback",
		mustJSON(t, map[string]any{"reference": "TRX-UNKNOWN", "status": payment.StatusPaid, "amount": 1}),
		func(request *http.Request) {
			request.Header.Set("X-Callback-Signature", testutil.ValidCallbackSignature)
		})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", recorder.Code, recorder.Body.String())
	}

	body := decodeJSON(t, recorder.Body.Bytes()).(map[string]any)
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("callback answer has no data object: %v", body)
	}
	assertSameKeys(t, map[string]any{"received": true}, data, "$.data")
}
