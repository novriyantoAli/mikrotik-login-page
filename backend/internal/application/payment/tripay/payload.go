package tripay

import (
	"fmt"
	"strings"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
)

// The wire types below mirror Tripay's envelope. They are unexported and never
// leave this package: a vendor field name appearing in a DTO would break ADR
// 0008's "no Tripay type crosses the port" rule.

type instructionPayload struct {
	Title string   `json:"title"`
	Steps []string `json:"steps"`
}

type createData struct {
	Reference   string `json:"reference"`
	MerchantRef string `json:"merchant_ref"`
	PaymentName string `json:"payment_name"`
	PayCode     string `json:"pay_code"`
	PayURL      string `json:"pay_url"`
	QRString    string `json:"qr_string"`
	QRURL       string `json:"qr_url"`
	Amount      int64  `json:"amount"`
	ExpiredTime int64  `json:"expired_time"`

	Instructions []instructionPayload `json:"instructions"`
}

type createEnvelope struct {
	Success bool       `json:"success"`
	Message string     `json:"message"`
	Data    createData `json:"data"`
}

type detailData struct {
	Reference   string `json:"reference"`
	MerchantRef string `json:"merchant_ref"`
	Status      string `json:"status"`
	Amount      int64  `json:"amount"`
	PaidAt      *int64 `json:"paid_at"`
}

type detailEnvelope struct {
	Success bool       `json:"success"`
	Message string     `json:"message"`
	Data    detailData `json:"data"`
}

type callbackData struct {
	Reference   string `json:"reference"`
	MerchantRef string `json:"merchant_ref"`
	Status      string `json:"status"`
	Amount      int64  `json:"amount"`
	PaidAt      *int64 `json:"paid_at"`
}

type callbackPayload struct {
	Success bool         `json:"success"`
	Event   string       `json:"event"`
	Data    callbackData `json:"data"`
}

type channelFeePayload struct {
	Flat    int64   `json:"flat"`
	Percent float64 `json:"percent"`
}

type channelPayload struct {
	Code        string            `json:"code"`
	Name        string            `json:"name"`
	Group       string            `json:"group"`
	Type        string            `json:"type"`
	Active      bool              `json:"active"`
	FeeCustomer channelFeePayload `json:"fee_customer"`
	// The vendor publishes the amount window per channel; a Package below the
	// minimum is answered "Minimum payment amount is Rp 10.000" after the fact.
	MinimumAmount int64 `json:"minimum_amount"`
	MaximumAmount int64 `json:"maximum_amount"`
}

type channelsEnvelope struct {
	Success bool             `json:"success"`
	Message string           `json:"message"`
	Data    []channelPayload `json:"data"`
}

// parseCreateResponse maps one vendor create answer onto the port's shape. When
// the vendor omits expired_time, the expiry this system asked for is kept, so a
// payment window is never silently longer than configured.
func parseCreateResponse(data createData, fallbackExpiry time.Time) *payment.CreateTransactionResponse {
	expiresAt := fallbackExpiry
	if data.ExpiredTime > 0 {
		expiresAt = time.Unix(data.ExpiredTime, 0).UTC()
	}

	instructions := make([]payment.Instruction, 0, len(data.Instructions))
	for _, instruction := range data.Instructions {
		instructions = append(instructions, payment.Instruction{
			Title: instruction.Title,
			Steps: append([]string(nil), instruction.Steps...),
		})
	}

	return &payment.CreateTransactionResponse{
		Reference:        data.MerchantRef,
		GatewayReference: data.Reference,
		ChannelName:      data.PaymentName,
		PayCode:          data.PayCode,
		PayURL:           data.PayURL,
		QRString:         data.QRString,
		QRURL:            data.QRURL,
		Instructions:     instructions,
		ExpiresAt:        expiresAt,
	}
}

// closedPaymentGroups are the Tripay groups checkout renders inside the app: a
// virtual account number the buyer pays from within the page. QRIS is checked by
// code, not by group, because the vendor files it under "E-Wallet" (verified
// against the sandbox) while its type is still `direct`. Retail and card channels
// are open payments that need a hosted redirect, which ADR 0008 rejects for this
// flow, so they are never offered even when the merchant has them active (PRD
// point 19).
var closedPaymentGroups = map[string]bool{
	"Virtual Account": true,
}

// qrisCode is the canonical QRIS channel. The vendor also publishes QRISC,
// QRIS2 and QRIS_SHOPEEPAY: the same rail and the same fee under acquirer-specific
// names, which are not four different choices for a buyer.
const qrisCode = "QRIS"

// isClosedPayment reports whether one channel can be paid from inside the app.
// `type` is the vendor's own answer to that question: `direct` means the app
// shows a code, `redirect` means the buyer would leave the page.
func isClosedPayment(row channelPayload) bool {
	if !row.Active || row.Code == "" || row.Type != "direct" {
		return false
	}
	return closedPaymentGroups[row.Group] ||
		strings.HasPrefix(strings.ToUpper(row.Code), qrisCode)
}

// parseChannels keeps the active closed-payment methods only: an inactive
// channel, or one that would leave the app, is not something checkout may offer.
// One QRIS rail is enough: the canonical code when the merchant has it, otherwise
// the first variant.
func parseChannels(rows []channelPayload) []payment.Channel {
	channels := make([]payment.Channel, 0, len(rows))
	var qris *payment.Channel

	for _, row := range rows {
		if !isClosedPayment(row) {
			continue
		}

		channel := payment.Channel{
			Code:  row.Code,
			Name:  row.Name,
			Group: row.Group,
			FeeCustomer: payment.ChannelFee{
				FlatIDR: row.FeeCustomer.Flat,
				Percent: row.FeeCustomer.Percent,
			},
			MinAmountIDR: row.MinimumAmount,
			MaxAmountIDR: row.MaximumAmount,
		}

		if closedPaymentGroups[row.Group] {
			channels = append(channels, channel)
			continue
		}
		if qris == nil || channel.Code == qrisCode {
			qris = &channel
		}
	}

	if qris != nil {
		channels = append(channels, *qris)
	}
	return channels
}

// mapStatus is the single place a vendor status becomes a domain status. An
// unknown value is an error, never a guess: a callback must not move a
// Transaction into a state this system did not define (PRD point 7).
func mapStatus(vendor string) (payment.Status, error) {
	switch strings.ToUpper(strings.TrimSpace(vendor)) {
	case "UNPAID":
		return payment.StatusUnpaid, nil
	case "PAID":
		return payment.StatusPaid, nil
	case "EXPIRED":
		return payment.StatusExpired, nil
	case "FAILED", "REFUND":
		// Refunds are out of scope for this slice; a refunded payment is not a
		// settled one, and treating it as failed keeps it out of provisioning.
		return payment.StatusFailed, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownStatus, vendor)
	}
}
