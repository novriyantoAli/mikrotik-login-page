// Package dto holds the only types that cross the HTTP boundary for a
// Transaction. Money is always integer rupiah and timestamps are RFC 3339, so the
// app never parses a formatted string to get a number (PRD point 19/28).
package dto

// CreateTransactionRequest asks for the pending Transaction of one Package. The
// Phone Number is optional and only ever a cross-check: the session decides
// whose Transaction this is (PRD point 13).
type CreateTransactionRequest struct {
	PackageID uint   `json:"package_id" binding:"required"`
	Channel   string `json:"channel" binding:"required"`
	// PhoneNumber is never used to select a buyer; a value that differs from the
	// session answers 404 so the endpoint cannot probe other buyers' data.
	PhoneNumber string `json:"phone_number"`
	// ReturnURL is the Login Page the buyer came from. It is accepted only when
	// it matches the deployment allowlist and is echoed back validated.
	ReturnURL string `json:"return_url"`
}

// ChannelResponse is one closed-payment method the checkout may offer. Fees are
// separate from the Package price and are never recomputed by the client.
type ChannelResponse struct {
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Group    string  `json:"group"`
	FeeFlat  int64   `json:"fee_flat_idr"`
	FeeRatio float64 `json:"fee_percent"`
}

// InstructionResponse is one block of payment instructions.
type InstructionResponse struct {
	Title string   `json:"title"`
	Steps []string `json:"steps"`
}

// ChannelFilter is the query the channel list accepts. PackageID is optional: set,
// the list keeps only the channels whose published amount window can carry that
// Package's price, so checkout never offers a method the vendor will refuse
// (ADR 0021).
type ChannelFilter struct {
	PackageID uint `form:"package_id"`
}

// TransactionResponse is the checkout's whole view of one Transaction: the
// amount, the channel, the instructions, the deadline and the state the app
// polls.
type TransactionResponse struct {
	ID        uint   `json:"id"`
	Reference string `json:"reference"`
	// PhoneNumber is the buyer's canonical identity and the hotspot username,
	// echoed so the completion screen can show exactly what to type (PRD point 55).
	PhoneNumber string `json:"phone_number"`
	PackageID   uint   `json:"package_id"`
	PackageName string `json:"package_name"`
	AmountIDR   int64  `json:"amount_idr"`
	Channel     string `json:"channel"`
	ChannelName string `json:"channel_name"`
	State       string `json:"state"`

	PayCode  string `json:"pay_code"`
	PayURL   string `json:"pay_url"`
	QRString string `json:"qr_string"`
	QRURL    string `json:"qr_url"`

	Instructions []InstructionResponse `json:"instructions"`

	ReturnURL string  `json:"return_url"`
	ExpiresAt string  `json:"expires_at"`
	PaidAt    *string `json:"paid_at"`
	CreatedAt string  `json:"created_at"`

	// PIN is the Phone Number's current hotspot credential, which is the newest
	// provisioned purchase's. It is filled only when this Transaction is
	// provisioned, and only on the single-Transaction read the buyer's own
	// session authorizes; no list response ever carries one (ADR 0016).
	PIN *string `json:"pin"`
}

// CallbackAck tells the gateway the notification was accepted. It carries no
// Transaction data: the callback endpoint is public, so its answer must not leak
// whether a reference exists.
type CallbackAck struct {
	Received bool `json:"received"`
}

// TransactionFilter is the query the history endpoint accepts. PhoneNumber is
// only ever a cross-check against the session, never a row selector (PRD point
// 13), and the page bounds keep one request from asking for the whole table.
type TransactionFilter struct {
	Page        int    `form:"page" binding:"omitempty,min=1"`
	PageSize    int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	PhoneNumber string `form:"phone_number"`
}

// TransactionSummaryResponse is one row of the Transaction history: when it
// happened, which Package, how much, and the state. It deliberately has no PIN
// field — a list never carries a credential (ADR 0017).
type TransactionSummaryResponse struct {
	ID          uint    `json:"id"`
	Reference   string  `json:"reference"`
	PackageID   uint    `json:"package_id"`
	PackageName string  `json:"package_name"`
	AmountIDR   int64   `json:"amount_idr"`
	Channel     string  `json:"channel"`
	ChannelName string  `json:"channel_name"`
	State       string  `json:"state"`
	CreatedAt   string  `json:"created_at"`
	PaidAt      *string `json:"paid_at"`
}

// TransactionPage is one page of history plus the numbers the app needs to
// paginate. The handler renders it as the shared `{data, total_count, page,
// page_size}` envelope.
type TransactionPage struct {
	Items      []TransactionSummaryResponse
	TotalCount int64
	Page       int
	PageSize   int
}

// AdminTransactionFilter is the panel's Transaction search: the filters an
// operator types while answering a buyer's call. Dates are `YYYY-MM-DD` days in
// the deployment's own day (WIB, see the service's panelDayLocation): From is the
// first instant of its day and To is the first instant of the day after it, so a
// whole day is always included and two days never overlap.
type AdminTransactionFilter struct {
	// State is not gated by a binding tag: the lifecycle owns the vocabulary
	// (`entity.State.Known`), so there is one list of states rather than one per
	// transport, and a typo answers the same 422 as any other refused filter.
	State       string `form:"state"`
	PhoneNumber string `form:"phone_number"`
	PackageID   uint   `form:"package_id"`
	From        string `form:"from"`
	To          string `form:"to"`
	Page        int    `form:"page" binding:"omitempty,min=1"`
	PageSize    int    `form:"page_size" binding:"omitempty,min=1,max=100"`
}

// AdminTransactionSummaryResponse is one row of the panel's Transaction list. It
// is deliberately not TransactionResponse: the panel answers support calls, so it
// also sees the buyer's Phone Number, the reference, the provisioning attempt
// count and the last SMS outcome — while the PIN is absent by construction, not by
// a nil value a later change could fill in (PRD #35).
//
// PackageName and AmountIDR are the snapshot the buyer paid for, never the current
// catalog row (ADR 0026).
type AdminTransactionSummaryResponse struct {
	ID                uint   `json:"id" example:"12"`
	Reference         string `json:"reference" example:"TRX-0123456789ABCDEF"`
	PhoneNumber       string `json:"phone_number" example:"6281234567890"`
	PackageID         uint   `json:"package_id" example:"1"`
	PackageName       string `json:"package_name" example:"Paket Harian"`
	AmountIDR         int64  `json:"amount_idr" example:"5000"`
	Channel           string `json:"channel" example:"BRIVA"`
	ChannelName       string `json:"channel_name" example:"BRIVA"`
	State             string `json:"state" example:"provisioned"`
	GatewayReference  string `json:"gateway_reference" example:"T0001"`
	ProvisionAttempts int    `json:"provision_attempts" example:"1"`
	// LastSMSState is the newest outbox row's state for this Phone Number:
	// pending, sending, sent or failed. It is nil when nothing was ever queued.
	LastSMSState *string `json:"last_sms_state" example:"sent"`
	LastSMSAt    *string `json:"last_sms_at" example:"2026-10-23T08:02:00Z"`
	CreatedAt    string  `json:"created_at" example:"2026-10-23T08:00:00Z"`
	PaidAt       *string `json:"paid_at" example:"2026-10-23T08:01:00Z"`
	ExpiresAt    string  `json:"expires_at" example:"2026-10-23T09:00:00Z"`
}

// AdminTransactionPage is one page of the panel's search plus the numbers the
// panel paginates with. The handler renders it as the shared envelope, so the
// panel learns one pagination shape for every list (PRD #35).
type AdminTransactionPage struct {
	Items      []AdminTransactionSummaryResponse
	TotalCount int64
	Page       int
	PageSize   int
}

// AdminPINRevealResponse is the one place a PIN travels towards the panel: a single
// field, produced only by the audited reveal endpoint, never by a list (ADR 0027 §4).
type AdminPINRevealResponse struct {
	PIN string `json:"pin" example:"123456"`
}
