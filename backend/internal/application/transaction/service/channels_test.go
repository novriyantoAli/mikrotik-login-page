package service_test

import (
	"context"
	"errors"
	"testing"

	packagesentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
)

// cheapPackageID is a Package below every virtual account's minimum, the shape
// that made checkout answer "Minimum payment amount is Rp 10.000".
const cheapPackageID = uint(8)

// withChannelWindows puts a Rp 5.000 Package and two channels with real amount
// windows into the fixture: a virtual account that starts at Rp 10.000 and a QRIS
// code that starts at Rp 1.000.
func withChannelWindows(f *fixture) {
	f.catalog.packages[cheapPackageID] = packagesentity.Package{
		ID: cheapPackageID, Name: "Paket Harian", PriceIDR: 5000,
		DurationMinutes: 1440, RateLimit: "5M/5M",
	}
	f.gateway.channels = []payment.Channel{
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
	}
}

func TestChannels_LeaveOutChannelsThePackagePriceCannotUse(t *testing.T) {
	t.Parallel()

	f := newFixture()
	withChannelWindows(f)
	ctx := context.Background()

	cheap, err := f.service.Channels(ctx, cheapPackageID)
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	if len(cheap) != 1 || cheap[0].Code != "QRIS" {
		t.Fatalf("channels for a Rp 5.000 Package = %+v, want QRIS only", cheap)
	}

	// The fixture's Paket Mingguan is Rp 25.000, inside both windows.
	rich, err := f.service.Channels(ctx, 7)
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	if len(rich) != 2 {
		t.Fatalf("channels for a Rp 25.000 Package = %+v, want both", rich)
	}

	// Without a Package the endpoint stays a plain channel list.
	all, err := f.service.Channels(ctx, 0)
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("channels without a Package = %d, want 2", len(all))
	}
}

func TestChannels_RejectsAnUnknownPackage(t *testing.T) {
	t.Parallel()

	f := newFixture()

	if _, err := f.service.Channels(context.Background(), 404); !errors.Is(err, service.ErrPackageUnknown) {
		t.Fatalf("Channels = %v, want ErrPackageUnknown", err)
	}
}

func TestCreate_RefusesAChannelWhoseWindowExcludesThePrice(t *testing.T) {
	t.Parallel()

	f := newFixture()
	withChannelWindows(f)

	_, err := f.service.Create(context.Background(), testPhone, &dto.CreateTransactionRequest{
		PackageID: cheapPackageID,
		Channel:   "BCAVA",
	})
	if !errors.Is(err, service.ErrChannelAmountUnsupported) {
		t.Fatalf("Create = %v, want ErrChannelAmountUnsupported", err)
	}
	if f.gateway.createCalls != 0 {
		t.Fatalf("the vendor was asked to open %d payments, want none", f.gateway.createCalls)
	}
	if len(f.repo.rows) != 0 {
		t.Fatalf("stored %d Transactions for an unusable channel, want none", len(f.repo.rows))
	}
}

func TestCreate_AcceptsAChannelThatFitsThePrice(t *testing.T) {
	t.Parallel()

	f := newFixture()
	withChannelWindows(f)

	created, err := f.service.Create(context.Background(), testPhone, &dto.CreateTransactionRequest{
		PackageID: cheapPackageID,
		Channel:   "QRIS",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Channel != "QRIS" || created.AmountIDR != 5000 {
		t.Fatalf("created = %+v, want a Rp 5.000 QRIS Transaction", created)
	}
}
