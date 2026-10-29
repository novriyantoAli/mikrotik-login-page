package payment_test

import (
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
)

func TestChannelAcceptsTheAmountInsideItsWindow(t *testing.T) {
	channel := payment.Channel{Code: "BCAVA", MinAmountIDR: 10000, MaxAmountIDR: 10000000}

	tests := []struct {
		name   string
		amount int64
		want   bool
	}{
		{name: "below the minimum", amount: 5000, want: false},
		{name: "on the minimum", amount: 10000, want: true},
		{name: "inside the window", amount: 25000, want: true},
		{name: "on the maximum", amount: 10000000, want: true},
		{name: "above the maximum", amount: 10000001, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := channel.Accepts(test.amount); got != test.want {
				t.Fatalf("Accepts(%d) = %v, want %v", test.amount, got, test.want)
			}
		})
	}
}

func TestChannelWithoutAPublishedWindowAcceptsEveryAmount(t *testing.T) {
	channel := payment.Channel{Code: "QRIS"}

	for _, amount := range []int64{1, 5000, 100000000} {
		if !channel.Accepts(amount) {
			t.Fatalf("Accepts(%d) = false; zero means \"no bound published\"", amount)
		}
	}
}
