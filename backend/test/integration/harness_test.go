package integration

import (
	"context"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// TestHarness_PostgresSchemaIsFreshPerTest_A and _B share one Postgres server
// but must never see each other's rows. Both create the same table and require
// it to start empty.
func TestHarness_PostgresSchemaIsFreshPerTest_A(t *testing.T) { assertFreshSchema(t) }

func TestHarness_PostgresSchemaIsFreshPerTest_B(t *testing.T) { assertFreshSchema(t) }

func assertFreshSchema(t *testing.T) {
	t.Helper()

	harness := testutil.NewAPIHarness(t)

	if err := harness.Postgres.Exec(`CREATE TABLE harness_probe (id integer PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create probe table: %v", err)
	}

	var count int64
	if err := harness.Postgres.Table("harness_probe").Count(&count).Error; err != nil {
		t.Fatalf("count probe rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("schema %q is not fresh: found %d leftover rows", harness.Schema, count)
	}

	if err := harness.Postgres.Exec(`INSERT INTO harness_probe (id) VALUES (1)`).Error; err != nil {
		t.Fatalf("insert probe row: %v", err)
	}
}

// TestHarness_VendorPortsAreFake proves the harness replaces every vendor edge
// behind its port, so later slices can drive transactions and provisioning
// without a payment provider, a modem or a FreeRADIUS database.
func TestHarness_VendorPortsAreFake(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()

	created, err := harness.Gateway.CreateTransaction(ctx, payment.CreateTransactionRequest{
		Reference:   "TRX-CONTRACT-1",
		Amount:      25000,
		PhoneNumber: "081200000001",
	})
	if err != nil {
		t.Fatalf("fake gateway: %v", err)
	}
	if created.Reference != "TRX-CONTRACT-1" {
		t.Fatalf("gateway reference = %q", created.Reference)
	}
	if _, ok := harness.Doubles.Gateway.LastCreateRequest(); !ok {
		t.Fatal("the injected payment.Gateway is not the fake")
	}

	if err := harness.SMSSender.Send(ctx, "081200000001", "PIN Anda 123456"); err != nil {
		t.Fatalf("fake sms sender: %v", err)
	}
	messages := harness.Doubles.SMS.Messages()
	if len(messages) != 1 || messages[0].PhoneNumber != "081200000001" {
		t.Fatalf("sms fake recorded %v", messages)
	}

	if err := harness.RadiusWriter.Apply(ctx, provisioning.Account{
		PhoneNumber: "081200000001",
		PIN:         "123456",
	}); err != nil {
		t.Fatalf("fake radius writer: %v", err)
	}
	if accounts := harness.Doubles.Radius.Accounts(); len(accounts) != 1 {
		t.Fatalf("radius fake recorded %v", accounts)
	}
}
