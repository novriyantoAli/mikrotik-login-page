package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
)

// referenceBytes is how much randomness one Transaction reference carries. 64
// bits is plenty for a value that only has to be unique, and stays short enough
// to read out of a support ticket.
const referenceBytes = 8

// newReference builds "TRX-" plus random hex. The gateway echoes it back as
// merchant_ref, so it must never be derived from a Phone Number or a counter.
func newReference() (string, error) {
	value := make([]byte, referenceBytes)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate transaction reference: %w", err)
	}
	return "TRX-" + strings.ToUpper(hex.EncodeToString(value)), nil
}

// toResponse maps the row onto the only shape the app sees. Timestamps are RFC
// 3339 so the app can render them in the buyer's locale, and money stays integer
// rupiah (PRD point 19). pinValue is non-nil only for a provisioned Transaction
// the caller is allowed to see the PIN of (ADR 0016).
func toResponse(row *entity.Transaction, pinValue *string) *dto.TransactionResponse {
	instructions := make([]dto.InstructionResponse, 0, len(row.Instructions))
	for _, instruction := range row.Instructions {
		instructions = append(instructions, dto.InstructionResponse{
			Title: instruction.Title,
			Steps: append([]string(nil), instruction.Steps...),
		})
	}

	return &dto.TransactionResponse{
		ID:           row.ID,
		Reference:    row.Reference,
		PhoneNumber:  row.PhoneNumber,
		PackageID:    row.PackageID,
		PackageName:  row.PackageName,
		AmountIDR:    row.AmountIDR,
		Channel:      row.Channel,
		ChannelName:  row.ChannelName,
		State:        string(row.State),
		PayCode:      row.PayCode,
		PayURL:       row.PayURL,
		QRString:     row.QRString,
		QRURL:        row.QRURL,
		Instructions: instructions,
		ReturnURL:    row.ReturnURL,
		ExpiresAt:    row.ExpiresAt.UTC().Format(time.RFC3339),
		PaidAt:       formatOptional(row.PaidAt),
		CreatedAt:    row.CreatedAt.UTC().Format(time.RFC3339),
		PIN:          pinValue,
	}
}

func formatOptional(moment *time.Time) *string {
	if moment == nil {
		return nil
	}
	formatted := moment.UTC().Format(time.RFC3339)
	return &formatted
}

// toSummary maps a row for the history list. There is no PIN argument on
// purpose: the list is the one Transaction shape that never carries a
// credential (ADR 0017).
func toSummary(row *entity.Transaction) dto.TransactionSummaryResponse {
	return dto.TransactionSummaryResponse{
		ID:          row.ID,
		Reference:   row.Reference,
		PackageID:   row.PackageID,
		PackageName: row.PackageName,
		AmountIDR:   row.AmountIDR,
		Channel:     row.Channel,
		ChannelName: row.ChannelName,
		State:       string(row.State),
		CreatedAt:   row.CreatedAt.UTC().Format(time.RFC3339),
		PaidAt:      formatOptional(row.PaidAt),
	}
}
