package integration

import (
	"bytes"
	"encoding/json"
	"testing"

	admindto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	packagesdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
)

// adminSessionEnvelope is the contract as Go sees it. The shared fixture and the
// panel's zod schema mirror this: an extra or missing field has to update the
// fixture, this DTO and the panel test together.
type adminSessionEnvelope struct {
	Data admindto.SessionResponse `json:"data"`
}

// adminListEnvelope is GET /api/admin/admins as the panel reads it.
type adminListEnvelope struct {
	Data []admindto.AdminResponse `json:"data"`
}

// TestAdminLoginContract_FixtureMatchesDTO proves the sign-in body the panel
// sends is exactly what the transport binds.
func TestAdminLoginContract_FixtureMatchesDTO(t *testing.T) {
	raw := loadContract(t, "admin-login.request.json")

	var request admindto.LoginRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		t.Fatalf("fixture does not decode into the login DTO: %v", err)
	}
	if request.Email == "" || request.Password == "" {
		t.Fatal("fixture must carry both email and password")
	}

	roundTrip, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal login DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, raw), decodeJSON(t, roundTrip), "$")
}

// TestAdminSessionContract_FixtureMatchesDTO keeps the one shape login and the
// session read share honest. There is no token field on purpose (ADR 0024 §2).
func TestAdminSessionContract_FixtureMatchesDTO(t *testing.T) {
	raw := loadContract(t, "admin-session.response.json")

	var envelope adminSessionEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("fixture does not decode into the session DTO: %v", err)
	}
	if envelope.Data.Email == "" || envelope.Data.Role == "" || envelope.Data.ExpiresAt.IsZero() {
		t.Fatalf("fixture session is incomplete: %+v", envelope.Data)
	}

	roundTrip, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal session DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, raw), decodeJSON(t, roundTrip), "$")
}

// TestAdminPackageWriteContract_FixtureMatchesDTO proves the two bodies the
// panel's Package form sends are exactly what the transport binds. The pair is
// deliberately asymmetric: `code` is the seed's identity, so it is required on
// create and only echoed on edit, where the server refuses a rename (ADR 0025
// §5). Without `version` an edit would overwrite a price nobody saw.
func TestAdminPackageWriteContract_FixtureMatchesDTO(t *testing.T) {
	createRaw := loadContract(t, "admin-package-create.request.json")

	var create packagesdto.CreatePackageRequest
	createDecoder := json.NewDecoder(bytes.NewReader(createRaw))
	createDecoder.DisallowUnknownFields()
	if err := createDecoder.Decode(&create); err != nil {
		t.Fatalf("fixture does not decode into the create DTO: %v", err)
	}
	if create.Code == "" || create.Name == "" || create.PriceIDR <= 0 ||
		create.DurationMinutes <= 0 || create.RateLimit == "" {
		t.Fatalf("fixture create body is incomplete: %+v", create)
	}

	createRoundTrip, err := json.Marshal(create)
	if err != nil {
		t.Fatalf("marshal create DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, createRaw), decodeJSON(t, createRoundTrip), "$")

	updateRaw := loadContract(t, "admin-package-update.request.json")

	var update packagesdto.UpdatePackageRequest
	updateDecoder := json.NewDecoder(bytes.NewReader(updateRaw))
	updateDecoder.DisallowUnknownFields()
	if err := updateDecoder.Decode(&update); err != nil {
		t.Fatalf("fixture does not decode into the update DTO: %v", err)
	}
	if update.Code == "" || update.Name == "" || update.PriceIDR <= 0 ||
		update.DurationMinutes <= 0 || update.RateLimit == "" {
		t.Fatalf("fixture update body is incomplete: %+v", update)
	}
	if update.Version <= 0 {
		t.Fatal("fixture edit carries no version, so it would overwrite a price nobody saw")
	}

	updateRoundTrip, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("marshal update DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, updateRaw), decodeJSON(t, updateRoundTrip), "$")
}

// TestAdminListContract_FixtureMatchesDTO proves the team screen cannot grow or
// lose a field without the fixture changing with it.
func TestAdminListContract_FixtureMatchesDTO(t *testing.T) {
	raw := loadContract(t, "admin-admins.response.json")

	var envelope adminListEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("fixture does not decode into the admins DTO: %v", err)
	}
	if len(envelope.Data) != 3 {
		t.Fatalf("fixture has %d admins, want one per role", len(envelope.Data))
	}
	for _, admin := range envelope.Data {
		if admin.ID == 0 || admin.Email == "" || admin.Role == "" {
			t.Fatalf("fixture admin is incomplete: %+v", admin)
		}
	}

	roundTrip, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal admins DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, raw), decodeJSON(t, roundTrip), "$")
}
