package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	adminentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	notificationentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/entity"
	packagesentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	transactionsdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	transactionentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// adminTransactionsEnvelope is GET /api/admin/transactions as the panel reads it.
type adminTransactionsEnvelope struct {
	Data       []transactionsdto.AdminTransactionSummaryResponse `json:"data"`
	TotalCount int64                                             `json:"total_count"`
	Page       int                                               `json:"page"`
	PageSize   int                                               `json:"page_size"`
}

// TestAdminTransactionContract_FixtureMatchesDTO keeps the shared fixture, the DTO
// and the panel's zod schema one shape. A field either side forgets fails here or
// there, and the list deliberately has no `pin` field to grow.
func TestAdminTransactionContract_FixtureMatchesDTO(t *testing.T) {
	raw := loadContract(t, "admin-transactions.response.json")

	var envelope adminTransactionsEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("fixture does not decode into the panel's Transaction DTO: %v", err)
	}
	if len(envelope.Data) != 1 {
		t.Fatalf("fixture carries %d rows, want 1", len(envelope.Data))
	}

	row := envelope.Data[0]
	if row.Reference == "" || row.PhoneNumber == "" || row.PackageName == "" ||
		row.LastSMSState == nil || row.ProvisionAttempts == 0 {
		t.Fatalf("fixture row is incomplete: %+v", row)
	}
	if envelope.TotalCount != 1 || envelope.Page != 1 || envelope.PageSize != 20 {
		t.Fatalf("fixture envelope = %+v", envelope)
	}

	roundTrip, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal the panel's Transaction envelope: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, raw), decodeJSON(t, roundTrip), "$")
}

// listPanelTransactions is the panel's search as an operator runs it.
func listPanelTransactions(
	t *testing.T,
	harness *testutil.APIHarness,
	cookie *http.Cookie,
	query url.Values,
) (*httptestResponse, adminTransactionsEnvelope) {
	t.Helper()

	path := "/api/admin/transactions"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	recorder := harness.DoRequest(t, http.MethodGet, path, nil, withCookie(cookie, ownerIP))

	envelope := adminTransactionsEnvelope{}
	if recorder.Code == http.StatusOK {
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode %s response %q: %v", path, recorder.Body.String(), err)
		}
	}
	return &httptestResponse{code: recorder.Code, body: recorder.Body.Bytes()}, envelope
}

// seedPanelTransaction writes one Transaction as the checkout would have left it:
// the catalog terms snapshotted on the row, at the given instant.
func seedPanelTransaction(
	t *testing.T,
	harness *testutil.APIHarness,
	row *transactionentity.Transaction,
) *transactionentity.Transaction {
	t.Helper()

	if row.Reference == "" {
		// One Transaction per reference: the row's identity, spelled out so two seeded
		// rows of the same buyer cannot collide on the unique index.
		row.Reference = fmt.Sprintf("TRX-PANEL-%s-%d-%d",
			row.PhoneNumber, row.PackageID, row.CreatedAt.Unix())
	}
	if row.PackageName == "" {
		row.PackageName = "Paket Harian"
	}
	if row.AmountIDR == 0 {
		row.AmountIDR = 5000
	}
	if row.Channel == "" {
		row.Channel = "BRIVA"
	}
	if row.ExpiresAt.IsZero() {
		row.ExpiresAt = row.CreatedAt.Add(time.Hour)
	}
	if err := harness.Postgres.Create(row).Error; err != nil {
		t.Fatalf("seed Transaction %s: %v", row.Reference, err)
	}
	return row
}

// panelTransactionAt is one row of the search with the fields the filters act on.
func panelTransactionAt(
	phoneNumber string,
	packageID uint,
	state transactionentity.State,
	at time.Time,
) *transactionentity.Transaction {
	return &transactionentity.Transaction{
		PhoneNumber:       phoneNumber,
		PackageID:         packageID,
		PackageName:       "Paket Harian",
		AmountIDR:         5000,
		DurationMinutes:   1440,
		RateLimit:         "5M/5M",
		Channel:           "BRIVA",
		ChannelName:       "BRIVA",
		State:             state,
		GatewayReference:  "GW-" + phoneNumber,
		ProvisionAttempts: 2,
		CreatedAt:         at,
	}
}

// TestAdminTransactionList_IsOpenToEveryRoleAndCarriesNoPIN is the search's column
// of ADR 0024 §3 plus #35's central rule: all three roles read it, and no row carries
// a credential — a list is never a place a PIN appears.
func TestAdminTransactionList_IsOpenToEveryRoleAndCarriesNoPIN(t *testing.T) {
	harness := newPanelHarness(t)
	ownerCookie := panelOwner(t, harness)
	_, ownerPassword := ownerCredentials()

	seedPanelTransaction(t, harness, panelTransactionAt(
		"6281200000101", 1, transactionentity.StateProvisioned,
		time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)))

	passwords := map[adminentity.Role]string{
		adminentity.RoleOwner:   ownerPassword,
		adminentity.RoleSupport: "kata-sandi-support-yang-panjang",
		adminentity.RoleAuditor: "kata-sandi-auditor-yang-panjang",
	}
	emails := map[adminentity.Role]string{
		adminentity.RoleOwner:   ownerCredentialsEmail(),
		adminentity.RoleSupport: "support-transaksi@example.test",
		adminentity.RoleAuditor: "auditor-transaksi@example.test",
	}
	for _, role := range []adminentity.Role{adminentity.RoleSupport, adminentity.RoleAuditor} {
		if response := createAdminRow(t, harness, ownerCookie, emails[role], string(role),
			passwords[role], ownerIP); response.code != http.StatusCreated {
			t.Fatalf("create %s = %d (%s)", role, response.code, response.body)
		}
	}

	for _, role := range []adminentity.Role{
		adminentity.RoleOwner, adminentity.RoleSupport, adminentity.RoleAuditor,
	} {
		cookie := mustAdminSession(t, harness, emails[role], passwords[role], ownerIP)
		response, envelope := listPanelTransactions(t, harness, cookie, nil)
		if response.code != http.StatusOK {
			t.Fatalf("%s GET /api/admin/transactions = %d (%s)", role, response.code, response.body)
		}
		if len(envelope.Data) != 1 {
			t.Fatalf("%s saw %d rows, want 1", role, len(envelope.Data))
		}

		// The envelope is the shared pagination shape, not a list under a bespoke key.
		var body map[string]any
		if err := json.Unmarshal(response.body, &body); err != nil {
			t.Fatalf("decode %s body: %v", role, err)
		}
		for _, key := range []string{"data", "total_count", "page", "page_size"} {
			if _, present := body[key]; !present {
				t.Fatalf("%s response is missing the pagination key %q: %s", role, key, response.body)
			}
		}

		for _, raw := range body["data"].([]any) {
			row := raw.(map[string]any)
			if _, leaked := row["pin"]; leaked {
				t.Fatalf("%s response carries a pin field: %s", role, response.body)
			}
		}
	}

	// No session at all is 401, never 403: the difference is "sign in", not "not you".
	if response, _ := listPanelTransactions(t, harness, nil, nil); response.code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d, want 401", response.code)
	}
}

// TestAdminTransactionList_FiltersAreAnsweredByTheSearch proves each filter the
// panel offers: state, Phone Number (in the form a buyer types it), Package and a
// day range, plus pagination.
func TestAdminTransactionList_FiltersAreAnsweredByTheSearch(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	seedPanelTransaction(t, harness, panelTransactionAt(
		"6281200000201", 1, transactionentity.StateProvisioned,
		time.Date(2026, time.October, 22, 23, 59, 0, 0, time.UTC)))
	seedPanelTransaction(t, harness, panelTransactionAt(
		"6281200000202", 2, transactionentity.StatePaid,
		time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)))
	seedPanelTransaction(t, harness, panelTransactionAt(
		"6281200000202", 1, transactionentity.StateExpired,
		time.Date(2026, time.October, 24, 0, 0, 0, 0, time.UTC)))

	cases := []struct {
		name  string
		query url.Values
		want  []string
	}{
		{
			name:  "everything, newest first",
			query: url.Values{},
			want:  []string{"6281200000202", "6281200000202", "6281200000201"},
		},
		{
			name:  "state",
			query: url.Values{"state": {"paid"}},
			want:  []string{"6281200000202"},
		},
		{
			name:  "phone number typed the national way",
			query: url.Values{"phone_number": {"081200000201"}},
			want:  []string{"6281200000201"},
		},
		{
			name:  "package",
			query: url.Values{"package_id": {"1"}},
			want:  []string{"6281200000202", "6281200000201"},
		},
		{
			// The day is WIB (UTC+7): 22 Oct 23:59Z is 23 Oct 06:59 WIB, so one
			// search for the 23rd returns both of that day's rows.
			name:  "one whole day, both bounds included",
			query: url.Values{"from": {"2026-10-23"}, "to": {"2026-10-23"}},
			want:  []string{"6281200000202", "6281200000201"},
		},
		{
			name:  "from a day onwards",
			query: url.Values{"from": {"2026-10-23"}},
			want:  []string{"6281200000202", "6281200000202", "6281200000201"},
		},
		{
			name:  "a filter that matches nothing is an empty page, not an error",
			query: url.Values{"state": {"failed"}},
			want:  []string{},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response, envelope := listPanelTransactions(t, harness, cookie, testCase.query)
			if response.code != http.StatusOK {
				t.Fatalf("status = %d (%s)", response.code, response.body)
			}

			got := make([]string, 0, len(envelope.Data))
			for _, row := range envelope.Data {
				got = append(got, row.PhoneNumber)
			}
			if len(got) != len(testCase.want) {
				t.Fatalf("rows = %v, want %v", got, testCase.want)
			}
			for index := range got {
				if got[index] != testCase.want[index] {
					t.Fatalf("rows = %v, want %v", got, testCase.want)
				}
			}
			if envelope.TotalCount != int64(len(testCase.want)) {
				t.Fatalf("total_count = %d, want %d", envelope.TotalCount, len(testCase.want))
			}
		})
	}

	// Pagination is the shared envelope, and page 2 of a two-row page is one row.
	response, envelope := listPanelTransactions(t, harness, cookie, url.Values{"page_size": {"2"}, "page": {"2"}})
	if response.code != http.StatusOK {
		t.Fatalf("paged list = %d (%s)", response.code, response.body)
	}
	if len(envelope.Data) != 1 || envelope.TotalCount != 3 || envelope.Page != 2 || envelope.PageSize != 2 {
		t.Fatalf("page 2 = %+v, want one row of three at page 2 with page_size 2", envelope)
	}
}

// TestAdminTransactionList_RefusesAFilterItCannotAnswer keeps a typo an operator can
// act on instead of a silently empty page.
func TestAdminTransactionList_RefusesAFilterItCannotAnswer(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	for name, query := range map[string]url.Values{
		"not a phone number": {"phone_number": {"bukan-nomor"}},
		"from is not a day":  {"from": {"23-10-2026"}},
		"range backwards":    {"from": {"2026-10-24"}, "to": {"2026-10-23"}},
		"state is not known": {"state": {"shipped"}},
	} {
		response, _ := listPanelTransactions(t, harness, cookie, query)
		if response.code != http.StatusUnprocessableEntity {
			t.Fatalf("%s = %d (%s), want the search's own 422", name, response.code, response.body)
		}

		var body httpx.ErrorResponse
		if err := json.Unmarshal(response.body, &body); err != nil {
			t.Fatalf("%s body is not the shared envelope: %v", name, err)
		}
		if body.Error.Code != httpx.CodeInvalidRequest {
			t.Fatalf("%s code = %q, want %q", name, body.Error.Code, httpx.CodeInvalidRequest)
		}
	}
}

// TestAdminTransactionList_ShowsTheSnapshotNotTheCatalog is #35's snapshot rule at
// the list boundary: editing the catalog after the sale must not rewrite the row a
// support operator reads back to the buyer.
func TestAdminTransactionList_ShowsTheSnapshotNotTheCatalog(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	var bought packagesentity.Package
	if err := harness.Postgres.Where("code = ?", "harian").First(&bought).Error; err != nil {
		t.Fatalf("read the seeded Package: %v", err)
	}

	seedPanelTransaction(t, harness, panelTransactionAt(
		"6281200000301", bought.ID, transactionentity.StateProvisioned,
		time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)))

	// The catalog is edited after the sale. The Transaction keeps what was sold.
	if err := harness.Postgres.Model(&packagesentity.Package{}).
		Where("id = ?", bought.ID).
		Updates(map[string]any{
			"name":       "Paket Harian Baru",
			"price_idr":  int64(9000),
			"rate_limit": "1M/1M",
		}).Error; err != nil {
		t.Fatalf("edit the catalog: %v", err)
	}

	response, envelope := listPanelTransactions(t, harness, cookie, nil)
	if response.code != http.StatusOK {
		t.Fatalf("list = %d (%s)", response.code, response.body)
	}
	if len(envelope.Data) != 1 {
		t.Fatalf("rows = %d, want 1", len(envelope.Data))
	}
	if envelope.Data[0].PackageName != "Paket Harian" || envelope.Data[0].AmountIDR != 5000 {
		t.Fatalf("row shows %q/%d, want the snapshot Paket Harian/5000",
			envelope.Data[0].PackageName, envelope.Data[0].AmountIDR)
	}
}

// TestAdminTransactionList_CarriesTheLastSMSStatusAndTheAttemptCount is the support
// answer to "did my PIN arrive?": the newest outbox row per Phone Number, whichever
// state it ended in, next to how often provisioning tried.
func TestAdminTransactionList_CarriesTheLastSMSStatusAndTheAttemptCount(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	at := time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)
	for index, phoneNumber := range []string{"6281200000401", "6281200000402", "6281200000403"} {
		row := panelTransactionAt(phoneNumber, uint(index+1), transactionentity.StateProvisioned,
			at.Add(time.Duration(index)*time.Minute))
		row.ProvisionAttempts = index + 1
		seedPanelTransaction(t, harness, row)
	}

	// Three numbers, three outcomes; the first one also has an older failed row that
	// the newest one must win over.
	for _, sms := range []*notificationentity.SMS{
		{
			PhoneNumber: "6281200000401", Message: "", State: notificationentity.StateFailed,
			NextAttemptAt: at, CreatedAt: at.Add(-10 * time.Minute), UpdatedAt: at.Add(-10 * time.Minute),
		},
		{
			PhoneNumber: "6281200000401", Message: "", State: notificationentity.StateSent,
			NextAttemptAt: at, CreatedAt: at, UpdatedAt: at,
		},
		{
			PhoneNumber: "6281200000402", Message: "PIN Anda", State: notificationentity.StatePending,
			NextAttemptAt: at, CreatedAt: at, UpdatedAt: at,
		},
		{
			PhoneNumber: "6281200000403", Message: "", State: notificationentity.StateFailed,
			NextAttemptAt: at, CreatedAt: at, UpdatedAt: at,
		},
	} {
		if err := harness.Postgres.Create(sms).Error; err != nil {
			t.Fatalf("seed sms row for %s: %v", sms.PhoneNumber, err)
		}
	}

	response, envelope := listPanelTransactions(t, harness, cookie, nil)
	if response.code != http.StatusOK {
		t.Fatalf("list = %d (%s)", response.code, response.body)
	}
	if len(envelope.Data) != 3 {
		t.Fatalf("rows = %d, want 3", len(envelope.Data))
	}

	byPhone := make(map[string]transactionsdto.AdminTransactionSummaryResponse, len(envelope.Data))
	for _, row := range envelope.Data {
		byPhone[row.PhoneNumber] = row
	}

	wantStates := map[string]string{
		"6281200000401": "sent",
		"6281200000402": "pending",
		"6281200000403": "failed",
	}
	for phoneNumber, want := range wantStates {
		row, present := byPhone[phoneNumber]
		if !present {
			t.Fatalf("%s is missing from the page", phoneNumber)
		}
		if row.LastSMSState == nil || *row.LastSMSState != want {
			t.Fatalf("%s last_sms_state = %v, want %q", phoneNumber, row.LastSMSState, want)
		}
		if row.LastSMSAt == nil {
			t.Fatalf("%s last_sms_at is empty for a row that has a message", phoneNumber)
		}
	}
	if byPhone["6281200000401"].ProvisionAttempts != 1 ||
		byPhone["6281200000403"].ProvisionAttempts != 3 {
		t.Fatalf("provision_attempts did not follow the rows: %+v", envelope.Data)
	}
}
