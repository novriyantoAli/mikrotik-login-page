// Package service holds the Transaction rules: checkout is idempotent per session
// and Package, only a verified gateway callback may mark a payment paid, the state
// machine refuses every other transition, and the deadline is the clock's, not
// the wall's (PRD points 15–20). No gin, no vendor type.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	packagesentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	packageservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/loginpage"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/phone"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/pin"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/queue"
)

// Errors the handler maps onto HTTP statuses. They are sentinels, not strings,
// so a transport never has to match message text.
var (
	// ErrPackageUnknown means the requested Package is not in the catalog.
	ErrPackageUnknown = errors.New("package not found")
	// ErrChannelUnknown means the gateway does not offer that channel.
	ErrChannelUnknown = errors.New("payment channel is not available")
	// ErrNotFound covers an unknown Transaction and one that belongs to another
	// Phone Number: the caller must not be able to tell them apart (PRD point 13).
	ErrNotFound = errors.New("transaction not found")
	// ErrAmountMismatch means a settlement callback carries a different amount
	// than the Transaction; it needs an operator, not a retry.
	ErrAmountMismatch = errors.New("callback amount does not match the transaction")
	// ErrGatewayUnavailable means the vendor could not be reached or refused the
	// request, which is a dependency failure, not a buyer error.
	ErrGatewayUnavailable = errors.New("payment gateway unavailable")
)

// Settings are the Transaction knobs, kept in a local struct so the domain does
// not depend on the deployment configuration struct.
type Settings struct {
	// PaymentTTL is how long a pending Transaction may be paid.
	PaymentTTL time.Duration
	// LoginPageAllowlist is the deployment's list of acceptable `return` targets.
	LoginPageAllowlist []string
}

// Service is the Transaction domain's public surface. #15 provisions from the
// `paid` state this service produces; it is out of scope here.
type Service interface {
	// Channels lists the closed-payment methods checkout may offer.
	Channels(ctx context.Context) ([]dto.ChannelResponse, error)
	// Create returns the live pending Transaction for this Package or opens a new
	// one. A repeated call never charges twice (PRD point 17).
	Create(ctx context.Context, phoneNumber string, request *dto.CreateTransactionRequest) (
		*dto.TransactionResponse, error)
	// Get returns one Transaction owned by phoneNumber, or ErrNotFound.
	Get(ctx context.Context, phoneNumber string, id uint) (*dto.TransactionResponse, error)
	// HandleCallback verifies a gateway notification and applies the single
	// allowed settlement transition. It is idempotent against repeats.
	HandleCallback(ctx context.Context, raw []byte, signature string) error
	// CurrentPIN returns the hotspot PIN of the newest provisioned Transaction of
	// a Phone Number, so the completion screen and Account Status read it the same
	// way (ADR 0016). found is false before the first successful purchase.
	CurrentPIN(ctx context.Context, phoneNumber string) (string, bool, error)
}

type service struct {
	repo     repository.Repository
	catalog  packageservice.Service
	gateway  payment.Gateway
	cipher   *pin.Cipher
	waker    queue.Enqueuer
	clock    clock.Clock
	settings Settings
	logger   *zap.Logger
}

// NewService builds the Transaction service; Fx injects every port.
func NewService(
	repo repository.Repository,
	catalog packageservice.Service,
	gateway payment.Gateway,
	cipher *pin.Cipher,
	waker queue.Enqueuer,
	clock clock.Clock,
	settings Settings,
	logger *zap.Logger,
) Service {
	return &service{
		repo:     repo,
		catalog:  catalog,
		gateway:  gateway,
		cipher:   cipher,
		waker:    waker,
		clock:    clock,
		settings: settings,
		logger:   logger,
	}
}

func (s *service) Channels(ctx context.Context) ([]dto.ChannelResponse, error) {
	channels, err := s.channelList(ctx)
	if err != nil {
		return nil, err
	}

	responses := make([]dto.ChannelResponse, 0, len(channels))
	for _, channel := range channels {
		responses = append(responses, dto.ChannelResponse{
			Code:     channel.Code,
			Name:     channel.Name,
			Group:    channel.Group,
			FeeFlat:  channel.FeeCustomer.FlatIDR,
			FeeRatio: channel.FeeCustomer.Percent,
		})
	}
	return responses, nil
}

func (s *service) Create(
	ctx context.Context,
	phoneNumber string,
	request *dto.CreateTransactionRequest,
) (*dto.TransactionResponse, error) {
	now := s.clock.Now()

	// A deadline that passed is final even before the worker sweeps it: closing
	// it here frees the one-pending index instead of failing the new checkout.
	if _, err := s.repo.ExpirePending(ctx, phoneNumber, request.PackageID, now); err != nil {
		return nil, err
	}

	existing, err := s.repo.Pending(ctx, phoneNumber, request.PackageID, now)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return s.response(ctx, existing), nil
	}

	pkg, channel, err := s.resolve(ctx, request)
	if err != nil {
		return nil, err
	}

	reference, err := newReference()
	if err != nil {
		return nil, err
	}

	expiresAt := now.Add(s.settings.PaymentTTL)
	created, err := s.gateway.CreateTransaction(ctx, payment.CreateTransactionRequest{
		Reference:   reference,
		Amount:      pkg.PriceIDR,
		PhoneNumber: phoneNumber,
		PackageName: pkg.Name,
		Channel:     channel.Code,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		s.logger.Error("create payment transaction",
			zap.String("reference", reference),
			zap.String("channel", channel.Code),
			zap.Error(err),
		)
		return nil, fmt.Errorf("%w: %w", ErrGatewayUnavailable, err)
	}

	row := s.newTransaction(phoneNumber, request, pkg.Name, pkg.PriceIDR, channel.Code, reference, created)
	if err := s.repo.Create(ctx, row); err != nil {
		// The partial unique index is the real idempotency guard. Losing that race
		// means another request already opened the pending Transaction: return it
		// and leave the orphaned vendor bill to expire on its own.
		if alive, findErr := s.repo.Pending(ctx, phoneNumber, request.PackageID, now); findErr == nil && alive != nil {
			s.logger.Info("checkout collapsed into the pending Transaction",
				zap.String("orphan_reference", reference),
				zap.Uint("transaction_id", alive.ID),
			)
			return s.response(ctx, alive), nil
		}
		return nil, fmt.Errorf("store transaction: %w", err)
	}

	s.logger.Info("transaction created",
		zap.String("reference", row.Reference),
		zap.String("channel", row.Channel),
		zap.Int64("amount_idr", row.AmountIDR),
	)
	return s.response(ctx, row), nil
}

func (s *service) Get(
	ctx context.Context,
	phoneNumber string,
	id uint,
) (*dto.TransactionResponse, error) {
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if row.PhoneNumber != phoneNumber {
		// Not forbidden: saying "exists but is not yours" would confirm another
		// buyer's Transaction (PRD point 13).
		return nil, ErrNotFound
	}
	return s.response(ctx, row), nil
}

// response maps a row for the buyer. The PIN is attached only once the account
// exists, so an unsettled checkout can never show a credential that FreeRADIUS
// does not hold yet (ADR 0016).
func (s *service) response(ctx context.Context, row *entity.Transaction) *dto.TransactionResponse {
	if row.State != entity.StateProvisioned || len(row.PINCiphertext) == 0 {
		return toResponse(row, nil)
	}

	pinValue, _, err := s.CurrentPIN(ctx, row.PhoneNumber)
	if err != nil {
		// The page still renders; the buyer keeps the SMS and can reload. The error
		// is logged without the ciphertext or the plaintext.
		s.logger.Error("read PIN for the checkout response",
			zap.Uint("transaction_id", row.ID), zap.Error(err))
		return toResponse(row, nil)
	}
	return toResponse(row, &pinValue)
}

func (s *service) CurrentPIN(ctx context.Context, phoneNumber string) (string, bool, error) {
	row, err := s.repo.LatestProvisioned(ctx, phoneNumber)
	if err != nil {
		return "", false, err
	}
	if row == nil || len(row.PINCiphertext) == 0 {
		return "", false, nil
	}

	pinValue, err := s.cipher.Decrypt(row.PINCiphertext, row.PINKeyVersion)
	if err != nil {
		s.logger.Error("decrypt current PIN",
			zap.String("phone_number", phone.Mask(phoneNumber)), zap.Error(err))
		return "", false, fmt.Errorf("decrypt current PIN: %w", err)
	}
	return pinValue, true, nil
}

// channelList is the one place the gateway's channel list is read and its
// failure is turned into a dependency error.
func (s *service) channelList(ctx context.Context) ([]payment.Channel, error) {
	channels, err := s.gateway.Channels(ctx)
	if err != nil {
		s.logger.Error("list payment channels", zap.Error(err))
		return nil, fmt.Errorf("%w: %w", ErrGatewayUnavailable, err)
	}
	return channels, nil
}

// resolve reads the Package and checks the channel against the gateway's list, so
// a buyer gets a clear 400 instead of a vendor rejection.
func (s *service) resolve(
	ctx context.Context,
	request *dto.CreateTransactionRequest,
) (*packagesentity.Package, *payment.Channel, error) {
	pkg, err := s.catalog.GetPackage(ctx, request.PackageID)
	switch {
	case errors.Is(err, packageservice.ErrPackageNotFound):
		return nil, nil, ErrPackageUnknown
	case err != nil:
		return nil, nil, fmt.Errorf("read Package: %w", err)
	}

	channel, err := s.findChannel(ctx, request.Channel)
	if err != nil {
		return nil, nil, err
	}
	return pkg, channel, nil
}

func (s *service) findChannel(ctx context.Context, code string) (*payment.Channel, error) {
	channels, err := s.channelList(ctx)
	if err != nil {
		return nil, err
	}

	for index := range channels {
		if strings.EqualFold(channels[index].Code, code) {
			return &channels[index], nil
		}
	}
	return nil, ErrChannelUnknown
}

func (s *service) newTransaction(
	phoneNumber string,
	request *dto.CreateTransactionRequest,
	packageName string,
	amountIDR int64,
	channel string,
	reference string,
	created *payment.CreateTransactionResponse,
) *entity.Transaction {
	instructions := make(entity.Instructions, 0, len(created.Instructions))
	for _, instruction := range created.Instructions {
		instructions = append(instructions, entity.Instruction{
			Title: instruction.Title,
			Steps: append([]string(nil), instruction.Steps...),
		})
	}

	if created.Reference != "" {
		// A gateway that echoes merchant_ref is authoritative about what it will
		// send back; both values are this system's reference in practice.
		reference = created.Reference
	}

	return &entity.Transaction{
		Reference:    reference,
		PhoneNumber:  phoneNumber,
		PackageID:    request.PackageID,
		PackageName:  packageName,
		AmountIDR:    amountIDR,
		Channel:      channel,
		ChannelName:  created.ChannelName,
		State:        entity.StatePending,
		PayCode:      created.PayCode,
		PayURL:       created.PayURL,
		QRString:     created.QRString,
		QRURL:        created.QRURL,
		Instructions: instructions,
		ReturnURL:    s.allowedReturn(request.ReturnURL),
		ExpiresAt:    created.ExpiresAt,
	}
}

// allowedReturn keeps a Login Page target only when the deployment allowlist
// accepts it, so the app never follows an unchecked URL (PRD point 27).
func (s *service) allowedReturn(candidate string) string {
	target, ok := loginpage.Allowed(candidate, s.settings.LoginPageAllowlist)
	if ok {
		return target
	}
	if strings.TrimSpace(candidate) != "" {
		s.logger.Warn("return target ignored: not in the Login Page allowlist")
	}
	return ""
}
