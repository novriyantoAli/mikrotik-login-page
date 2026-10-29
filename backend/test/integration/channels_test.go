package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// channelWindows replaces the fake vendor's channel list with two real windows: a
// virtual account that starts at Rp 10.000 and a QRIS code that starts at Rp
// 1.000. The seeded catalog's cheapest Package is Rp 5.000, which is exactly the
// production shape that made Tripay answer "Minimum payment amount is Rp 10.000".
func channelWindows(harness *testutil.APIHarness) {
	harness.Doubles.Gateway.SetChannelList([]payment.Channel{
		{
			Code: "BCAVA", Name: "BCA Virtual Account", Group: "Virtual Account",
			FeeCustomer:  payment.ChannelFee{FlatIDR: 5500},
			MinAmountIDR: 10000, MaxAmountIDR: 10000000,
		},
		{
			Code: "QRIS", Name: "QRIS", Group: "QRIS",
			FeeCustomer:  payment.ChannelFee{Percent: 0.7},
			MinAmountIDR: 1000, MaxAmountIDR: 5000000,
		},
	})
}

func listChannels(
	t *testing.T,
	harness *testutil.APIHarness,
	cookie *http.Cookie,
	path string,
) []dto.ChannelResponse {
	t.Helper()

	recorder := harness.DoRequest(t, http.MethodGet, path, nil, withCookie(cookie, "198.51.100.141"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d (%s)", path, recorder.Code, recorder.Body.String())
	}

	var envelope struct {
		Data []dto.ChannelResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode channels: %v", err)
	}
	return envelope.Data
}

// TestChannels_OnlyOffersTheChannelsThePackagePriceCanUse proves the buyer never
// sees a channel the vendor would refuse for this amount.
func TestChannels_OnlyOffersTheChannelsThePackagePriceCanUse(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	// Its own Phone Number and client IP: the Magic Link limiter allows one link per
	// number per window, so sharing a number with another test starves whichever
	// runs second of its sms_outbox row.
	cookie := newSessionCookie(t, harness, "081300000071", "198.51.100.61")
	channelWindows(harness)

	packageID := firstPackageID(t, harness)
	pkg, err := harness.Catalog.GetPackage(context.Background(), packageID)
	if err != nil {
		t.Fatalf("read Package: %v", err)
	}
	if pkg.PriceIDR >= 10000 {
		t.Skipf("the cheapest Package is Rp %d; filtering needs one below the VA minimum",
			pkg.PriceIDR)
	}

	all := listChannels(t, harness, cookie, "/api/transactions/channels")
	if len(all) != 2 {
		t.Fatalf("unfiltered channels = %+v, want both", all)
	}

	filtered := listChannels(t, harness, cookie,
		"/api/transactions/channels?package_id="+strconv.FormatUint(uint64(packageID), 10))
	if len(filtered) != 1 || filtered[0].Code != "QRIS" {
		t.Fatalf("channels for a Rp %d Package = %+v, want QRIS only", pkg.PriceIDR, filtered)
	}
}

// TestCheckout_ChannelWhoseWindowExcludesThePriceIsABadRequest pins the answer a
// stale app (or a hand-made request) gets: a buyer error naming the constraint,
// and no vendor call, instead of 502 "gateway unavailable".
func TestCheckout_ChannelWhoseWindowExcludesThePriceIsABadRequest(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000072", "198.51.100.62")
	channelWindows(harness)

	packageID := firstPackageID(t, harness)
	pkg, err := harness.Catalog.GetPackage(context.Background(), packageID)
	if err != nil {
		t.Fatalf("read Package: %v", err)
	}
	if pkg.PriceIDR >= 10000 {
		t.Skipf("the cheapest Package is Rp %d; this test needs one below the VA minimum",
			pkg.PriceIDR)
	}

	recorder := harness.DoRequest(t, http.MethodPost, "/api/transactions",
		mustJSON(t, map[string]any{"package_id": packageID, "channel": "BCAVA"}),
		withCookie(cookie, "198.51.100.162"))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/transactions = %d (%s)", recorder.Code, recorder.Body.String())
	}
	if last, ok := harness.Doubles.Gateway.LastCreateRequest(); ok {
		t.Fatalf("the vendor was asked to open a payment for %+v", last)
	}
}
