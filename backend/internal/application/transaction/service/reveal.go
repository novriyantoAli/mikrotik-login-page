// This file is the panel's one write: opening a buyer's PIN. It is a read of the
// buyer's data and a write to the trail at the same time, which is why it is
// separate from the panel's search — a list must never be able to grow this path
// (PRD admin-panel slice 2, ADR 0027 §4).
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
)

// The four outcomes one reveal attempt can end in, recorded in the trail's payload
// so a reader can tell them apart without reading a log line (ADR 0027 §3). They are
// stable strings: the audit reader filters on them.
const (
	pinRevealOK            = "ok"
	pinRevealNotFound      = "not_found"
	pinRevealNotReady      = "not_ready"
	pinRevealDecryptFailed = "decrypt_failed"
)

// RevealPIN opens one provisioned Transaction's PIN for an operator and leaves
// exactly one audit row for the attempt (ADR 0027 §4). The attempt is audited even
// when it fails, and the row never carries a plaintext PIN.
//
// After a successful decrypt the row is written before the value is returned: the
// trail is the price of reading a credential, so a trail that cannot be written
// refuses the reveal rather than handing out an unwitnessed PIN (fail closed).
func (s *service) RevealPIN(ctx context.Context, actor audit.Actor, id uint) (string, error) {
	if actor.ID == 0 {
		return "", ErrActorRequired
	}

	row, err := s.repo.GetByID(ctx, id)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		// Someone asked for the PIN of a Transaction that is not there. That is
		// still a fact about a privileged action, so it is kept (ADR 0027 §4). Its
		// own failure is logged by the helper: the answer the operator needs is the
		// not-found one, not "the trail is also down".
		_ = s.recordPINReveal(ctx, actor, id, "", pinRevealNotFound)
		return "", ErrNotFound
	case err != nil:
		return "", err
	}

	if row.State != entity.StateProvisioned || len(row.PINCiphertext) == 0 {
		_ = s.recordPINReveal(ctx, actor, id, row.Reference, pinRevealNotReady)
		return "", fmt.Errorf("%w: transaction %d is %s", ErrPINNotReady, id, row.State)
	}

	value, err := s.cipher.Decrypt(row.PINCiphertext, row.PINKeyVersion)
	if err != nil {
		// Neither the ciphertext nor a candidate plaintext: the identity only.
		s.logger.Error("decrypt PIN for the panel",
			zap.Uint("transaction_id", row.ID),
			zap.Int("pin_key_version", row.PINKeyVersion),
			zap.Error(err),
		)
		_ = s.recordPINReveal(ctx, actor, id, row.Reference, pinRevealDecryptFailed)
		return "", fmt.Errorf("%w: transaction %d", ErrPINUnavailable, id)
	}

	if err := s.recordPINReveal(ctx, actor, id, row.Reference, pinRevealOK); err != nil {
		return "", err
	}
	return value, nil
}

// recordPINReveal appends the one row this attempt owes. Its error is the caller's
// on the success path (no PIN without its witness) and is only logged on a path
// that already refuses: a second failure must not replace the reason the operator
// needs to read.
func (s *service) recordPINReveal(
	ctx context.Context,
	actor audit.Actor,
	id uint,
	reference string,
	result string,
) error {
	payload := map[string]any{"result": result}
	if reference != "" {
		payload["reference"] = reference
	}

	entry := audit.Entry{
		Actor:      actor,
		Action:     audit.ActionPINReveal,
		TargetType: audit.TargetTransaction,
		TargetID:   strconv.FormatUint(uint64(id), 10),
		Payload:    payload,
	}
	if err := s.repo.RecordPINReveal(ctx, entry); err != nil {
		s.logger.Error("record PIN reveal attempt",
			zap.Uint("transaction_id", id),
			zap.String("result", result),
			zap.Error(err),
		)
		return err
	}
	return nil
}
