package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	transactionsdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	transactionentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/pin"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// adminPINRevealEnvelope is POST /api/admin/transactions/{id}/pin as the panel
// reads it: one field, in the shared envelope.
type adminPINRevealEnvelope struct {
	Data transactionsdto.AdminPINRevealResponse `json:"data"`
}

// TestAdminPINRevealContract_FixtureMatchesDTO keeps the shared fixture, the DTO and
// the panel's zod schema one shape (PRD Testing Decisions). It is also the guard
// against this response growing a second field.
func TestAdminPINRevealContract_FixtureMatchesDTO(t *testing.T) {
	raw := loadContract(t, "admin-pin-reveal.response.json")

	var envelope adminPINRevealEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("fixture does not decode into the reveal DTO: %v", err)
	}
	if len(envelope.Data.PIN) != pin.Length {
		t.Fatalf("fixture pin = %q, want %d digits", envelope.Data.PIN, pin.Length)
	}

	roundTrip, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal the reveal envelope: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, raw), decodeJSON(t, roundTrip), "$")
}

// sealPIN is the fixture's own AEAD, built from the deployment keys the harness
// loaded, so a seeded Transaction carries the ciphertext provisioning would write.
func sealPIN(t *testing.T, harness *testutil.APIHarness, plaintext string) ([]byte, int) {
	t.Helper()

	cipher, err := pin.NewCipher(pin.Settings{
		Keys:          harness.Config.PIN.EncryptionKeys,
		ActiveVersion: harness.Config.PIN.ActiveKeyVersion,
	})
	if err != nil {
		t.Fatalf("build the PIN cipher from the harness config: %v", err)
	}
	ciphertext, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("seal the test PIN: %v", err)
	}
	return ciphertext, cipher.ActiveVersion()
}

// seedProvisionedWithPIN writes the Transaction a finished purchase leaves behind:
// provisioned, with the buyer's PIN sealed on the row (ADR 0016).
func seedProvisionedWithPIN(
	t *testing.T,
	harness *testutil.APIHarness,
	phoneNumber string,
	pinValue string,
) *transactionentity.Transaction {
	t.Helper()

	ciphertext, keyVersion := sealPIN(t, harness, pinValue)
	row := seedPanelTransaction(t, harness, panelTransactionAt(
		phoneNumber, 1, transactionentity.StateProvisioned,
		time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)))
	row.PINCiphertext = ciphertext
	row.PINKeyVersion = keyVersion
	if err := harness.Postgres.Model(&transactionentity.Transaction{}).
		Where("id = ?", row.ID).
		Updates(map[string]any{
			"pin_ciphertext":  ciphertext,
			"pin_key_version": keyVersion,
		}).Error; err != nil {
		t.Fatalf("seal the seeded Transaction's PIN: %v", err)
	}
	return row
}

// revealPIN is the panel's one write, as an operator runs it.
func revealPIN(
	t *testing.T,
	harness *testutil.APIHarness,
	cookie *http.Cookie,
	id uint,
) *httptestResponse {
	t.Helper()

	path := "/api/admin/transactions/" + strconv.FormatUint(uint64(id), 10) + "/pin"
	recorder := harness.DoRequest(t, http.MethodPost, path, nil, withCookie(cookie, ownerIP))
	return &httptestResponse{code: recorder.Code, body: recorder.Body.Bytes()}
}

// pinRevealRows keeps only the trail rows this act writes, so a test can count them
// without depending on what else the panel did.
func pinRevealRows(t *testing.T, harness *testutil.APIHarness) []entity.AuditEntry {
	t.Helper()

	rows := make([]entity.AuditEntry, 0, 1)
	for _, row := range readAuditRows(t, harness) {
		if row.Action == audit.ActionPINReveal {
			rows = append(rows, row)
		}
	}
	return rows
}

// TestAdminPINReveal_OwnerAndSupportReadTheSealedPIN is #36's center at the HTTP
// boundary: owner and support get the digits the buyer was sent, the answer is the
// shared envelope, and the PIN never reaches a list.
func TestAdminPINReveal_OwnerAndSupportReadTheSealedPIN(t *testing.T) {
	harness := newPanelHarness(t)
	ownerCookie := panelOwner(t, harness)

	supportEmail, supportPassword := "support-pin@example.test", "kata-sandi-support-yang-panjang"
	if response := createAdminRow(t, harness, ownerCookie, supportEmail, "support",
		supportPassword, ownerIP); response.code != http.StatusCreated {
		t.Fatalf("create support = %d (%s)", response.code, response.body)
	}
	supportCookie := mustAdminSession(t, harness, supportEmail, supportPassword, ownerIP)

	row := seedProvisionedWithPIN(t, harness, "6281200000701", "654321")

	for name, cookie := range map[string]*http.Cookie{
		"owner":   ownerCookie,
		"support": supportCookie,
	} {
		response := revealPIN(t, harness, cookie, row.ID)
		if response.code != http.StatusOK {
			t.Fatalf("%s reveal = %d (%s)", name, response.code, response.body)
		}

		var envelope adminPINRevealEnvelope
		if err := json.Unmarshal(response.body, &envelope); err != nil {
			t.Fatalf("%s decode reveal %q: %v", name, response.body, err)
		}
		if envelope.Data.PIN != "654321" {
			t.Fatalf("%s revealed %q, want the buyer's PIN", name, envelope.Data.PIN)
		}
	}

	// The list the same operator reads carries no PIN, before or after a reveal.
	listResponse, list := listPanelTransactions(t, harness, ownerCookie, nil)
	if listResponse.code != http.StatusOK || len(list.Data) != 1 {
		t.Fatalf("the panel's list = %d with %d rows, want one row",
			listResponse.code, len(list.Data))
	}
	var listBody struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(listResponse.body, &listBody); err != nil {
		t.Fatalf("decode the panel's list: %v", err)
	}
	if _, leaked := listBody.Data[0]["pin"]; leaked {
		t.Fatalf("the panel's list grew a pin field: %s", listResponse.body)
	}
}

// TestAdminPINReveal_AuditorIsRefusedAndNothingIsRecorded is the role matrix at the
// server, not in the UI: the person who reviews the trail may not act on it.
func TestAdminPINReveal_AuditorIsRefusedAndNothingIsRecorded(t *testing.T) {
	harness := newPanelHarness(t)
	ownerCookie := panelOwner(t, harness)

	auditorEmail, auditorPassword := "auditor-pin@example.test", "kata-sandi-auditor-yang-panjang"
	if response := createAdminRow(t, harness, ownerCookie, auditorEmail, "auditor",
		auditorPassword, ownerIP); response.code != http.StatusCreated {
		t.Fatalf("create auditor = %d (%s)", response.code, response.body)
	}
	auditorCookie := mustAdminSession(t, harness, auditorEmail, auditorPassword, ownerIP)

	row := seedProvisionedWithPIN(t, harness, "6281200000702", "111222")

	response := revealPIN(t, harness, auditorCookie, row.ID)
	if response.code != http.StatusForbidden {
		t.Fatalf("auditor reveal = %d (%s), want 403", response.code, response.body)
	}
	if rows := pinRevealRows(t, harness); len(rows) != 0 {
		t.Fatalf("a refused role left %d audit rows, want none", len(rows))
	}

	// No session at all is 401, never 403: the difference is "sign in", not "not you".
	if anonymous := revealPIN(t, harness, nil, row.ID); anonymous.code != http.StatusUnauthorized {
		t.Fatalf("anonymous reveal = %d, want 401", anonymous.code)
	}
}

// TestAdminPINReveal_EveryAttemptLeavesOneRowWithoutThePIN is ADR 0027 §4: the
// attempt is the fact, so success, a key fault and a repeat are all recorded, and no
// row carries the digits.
func TestAdminPINReveal_EveryAttemptLeavesOneRowWithoutThePIN(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	row := seedProvisionedWithPIN(t, harness, "6281200000703", "999888")

	for attempt := 1; attempt <= 2; attempt++ {
		if response := revealPIN(t, harness, cookie, row.ID); response.code != http.StatusOK {
			t.Fatalf("reveal %d = %d (%s)", attempt, response.code, response.body)
		}
	}

	// A row that names a key this deployment does not hold: the answer is generic and
	// the attempt is still kept (ADR 0016, ADR 0027 §4).
	broken, _ := sealPIN(t, harness, "777666")
	if err := harness.Postgres.Model(&transactionentity.Transaction{}).
		Where("id = ?", row.ID).
		Updates(map[string]any{"pin_ciphertext": broken, "pin_key_version": 9}).Error; err != nil {
		t.Fatalf("poison the seeded PIN: %v", err)
	}
	failed := revealPIN(t, harness, cookie, row.ID)
	if failed.code != http.StatusInternalServerError {
		t.Fatalf("reveal with an unknown key = %d (%s), want a generic 500",
			failed.code, failed.body)
	}
	var errorBody httpx.ErrorResponse
	if err := json.Unmarshal(failed.body, &errorBody); err != nil {
		t.Fatalf("decode the refusal: %v", err)
	}
	if errorBody.Error.Code != httpx.CodeInternal ||
		errorBody.Error.Message != "Terjadi kesalahan pada server." {
		t.Fatalf("refusal = %+v, want the shared generic sentence", errorBody.Error)
	}

	rows := pinRevealRows(t, harness)
	if len(rows) != 3 {
		t.Fatalf("audit rows = %d, want one per attempt", len(rows))
	}

	wantedTarget := strconv.FormatUint(uint64(row.ID), 10)
	wantResults := []string{"ok", "ok", "decrypt_failed"}
	for index, entry := range rows {
		if entry.TargetType != string(audit.TargetTransaction) || entry.TargetID != wantedTarget {
			t.Fatalf("row %d target = %q/%q, want transaction/%s",
				index, entry.TargetType, entry.TargetID, wantedTarget)
		}
		if entry.AdminEmail == "" {
			t.Fatalf("row %d names no actor", index)
		}
		if got := entry.Payload["result"]; got != wantResults[index] {
			t.Fatalf("row %d result = %v, want %q", index, got, wantResults[index])
		}

		encoded := mustJSONBytes(t, entry.Payload)
		if bytes.Contains(encoded, []byte("999888")) || bytes.Contains(encoded, []byte("777666")) {
			t.Fatalf("row %d carries a plaintext PIN: %s", index, encoded)
		}
	}
}

// TestAdminPINReveal_RefusesATransactionWithoutAPIN keeps "no PIN yet" a conflict an
// operator can retry, not a key fault and not a 404 that hides the Transaction.
func TestAdminPINReveal_RefusesATransactionWithoutAPIN(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	row := seedPanelTransaction(t, harness, panelTransactionAt(
		"6281200000704", 1, transactionentity.StatePaid,
		time.Date(2026, time.October, 23, 8, 30, 0, 0, time.UTC)))

	response := revealPIN(t, harness, cookie, row.ID)
	if response.code != http.StatusConflict {
		t.Fatalf("reveal of an unprovisioned Transaction = %d (%s), want 409",
			response.code, response.body)
	}

	var body httpx.ErrorResponse
	if err := json.Unmarshal(response.body, &body); err != nil {
		t.Fatalf("decode the refusal: %v", err)
	}
	if body.Error.Code != httpx.CodeConflict {
		t.Fatalf("code = %q, want %q", body.Error.Code, httpx.CodeConflict)
	}

	rows := pinRevealRows(t, harness)
	if len(rows) != 1 || rows[0].Payload["result"] != "not_ready" {
		t.Fatalf("audit rows = %+v, want one not_ready row", rows)
	}
}

// TestAdminPINReveal_UnknownTransactionIsNotFoundAndStillAudited: the caller cannot
// tell an id that never existed from one that was never theirs, but the attempt is
// still worth a row (ADR 0027 §4).
func TestAdminPINReveal_UnknownTransactionIsNotFoundAndStillAudited(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	response := revealPIN(t, harness, cookie, 999999)
	if response.code != http.StatusNotFound {
		t.Fatalf("reveal of an unknown id = %d (%s), want 404", response.code, response.body)
	}

	rows := pinRevealRows(t, harness)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want exactly 1", len(rows))
	}
	if rows[0].Payload["result"] != "not_found" {
		t.Fatalf("payload = %v, want the not-found outcome", rows[0].Payload)
	}
}

// TestAdminPINReveal_RefusesAnIDThatIsNotANumber keeps a typo the transport's own
// refusal, before any lookup or trail row.
func TestAdminPINReveal_RefusesAnIDThatIsNotANumber(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	recorder := harness.DoRequest(t, http.MethodPost,
		"/api/admin/transactions/dua/pin", nil, withCookie(cookie, ownerIP))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("reveal with id=dua = %d (%s), want 400", recorder.Code, recorder.Body.String())
	}
	if rows := pinRevealRows(t, harness); len(rows) != 0 {
		t.Fatalf("a malformed id left %d audit rows, want none", len(rows))
	}
}

// TestAdminPINReveal_CostNothingToTheBuyer is the scope guard: revealing a PIN reads
// the row and writes the trail, it never changes the buyer's account or expiry.
func TestAdminPINReveal_CostNothingToTheBuyer(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	row := seedProvisionedWithPIN(t, harness, "6281200000705", "121212")

	var before transactionentity.Transaction
	if err := harness.Postgres.First(&before, row.ID).Error; err != nil {
		t.Fatalf("read the Transaction: %v", err)
	}

	if response := revealPIN(t, harness, cookie, row.ID); response.code != http.StatusOK {
		t.Fatalf("reveal = %d (%s)", response.code, response.body)
	}

	var after transactionentity.Transaction
	if err := harness.Postgres.First(&after, row.ID).Error; err != nil {
		t.Fatalf("reread the Transaction: %v", err)
	}
	if after.State != before.State || !after.ExpiresAt.Equal(before.ExpiresAt) ||
		after.ProvisionAttempts != before.ProvisionAttempts {
		t.Fatalf("the reveal changed the Transaction: before %+v, after %+v", before, after)
	}
}

// mustJSONBytes marshals a value or fails the test.
func mustJSONBytes(t *testing.T, value any) []byte {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %T: %v", value, err)
	}
	return encoded
}
