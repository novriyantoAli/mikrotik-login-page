package integration

import (
	"bytes"
	"encoding/json"
	"testing"

	admindto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
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
