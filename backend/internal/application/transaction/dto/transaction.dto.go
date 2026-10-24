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
