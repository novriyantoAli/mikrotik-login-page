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
	Active      bool              `json:"active"`
	FeeCustomer channelFeePayload `json:"fee_customer"`
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
		Reference:    data.MerchantRef,
		ChannelName:  data.PaymentName,
		PayCode:      data.PayCode,
		PayURL:       data.PayURL,
		QRString:     data.QRString,
		QRURL:        data.QRURL,
		Instructions: instructions,
		ExpiresAt:    expiresAt,
	}
}

// parseChannels keeps the active closed-payment methods only: an inactive
// channel is not something the checkout may offer.
func parseChannels(rows []channelPayload) []payment.Channel {
	channels := make([]payment.Channel, 0, len(rows))
	for _, row := range rows {
		if !row.Active || row.Code == "" {
			continue
		}
		channels = append(channels, payment.Channel{
			Code:  row.Code,
			Name:  row.Name,
			Group: row.Group,
			FeeCustomer: payment.ChannelFee{
				FlatIDR: row.FeeCustomer.Flat,
				Percent: row.FeeCustomer.Percent,
			},
		})
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
