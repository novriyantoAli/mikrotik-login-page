// This file holds the panel's Transaction read surface: the search an operator runs
// while answering a buyer's call. It is a read-only view — the panel never changes a
// Transaction — and it is deliberately separate from the buyer's History so neither
// contract can grow a field the other must not have (PRD #35).
package service

import (
	"context"
	"errors"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/phone"
)

// ErrFilterInvalid is the panel's "this search cannot be answered" sentinel. The
// handler maps it to one 422 with the reason, and a caller branches on the value
// rather than on message text.
var ErrFilterInvalid = errors.New("transaction filter is invalid")

// Panel page bounds, the same numbers the buyer's history uses so the panel has one
// pagination shape to learn (PRD #35).
const (
	defaultPanelPageSize = 20
	maxPanelPageSize     = 100
)

// panelDateLayout is the day format the panel's date inputs send. The search would
// be unusable if this accepted arbitrary timestamps: an operator asks for "23
// October", not for an instant.
const panelDateLayout = time.DateOnly

// panelDayLocation is the day boundary a search means. It is the deployment's own
// day (WIB), not UTC: an operator answering a call at 08:00 WIB and the panel
// rendering that same instant must agree on which day a Transaction belongs to.
// The panel renders timestamps in this zone too, so the filter never disagrees with
// the column the operator just read.
var panelDayLocation = time.FixedZone("WIB", 7*60*60)

// FilterError names the refused filter and why, in the words the panel shows. It
// implements Is so every rule reads as ErrFilterInvalid while the message still
// tells the operator what to fix.
type FilterError struct {
	Field  string
	Reason string
}

// Error is the operator-facing reason, never a Go value dump.
func (e *FilterError) Error() string { return e.Reason }

// Is makes errors.Is(err, ErrFilterInvalid) true for every filter rule.
func (e *FilterError) Is(target error) bool { return target == ErrFilterInvalid }

// ListTransactions answers the panel's search. It reads the page once and the SMS
// statuses once for the whole page, so a page of a hundred rows is two queries, not
// a hundred.
func (s *service) ListTransactions(
	ctx context.Context,
	filter *dto.AdminTransactionFilter,
) (*dto.AdminTransactionPage, error) {
	params, err := panelParams(filter)
	if err != nil {
		return nil, err
	}

	rows, total, err := s.repo.ListForPanel(ctx, params)
	if err != nil {
		return nil, err
	}

	statuses, err := s.sms.LatestStatuses(ctx, panelPhoneNumbers(rows))
	if err != nil {
		// The rows are the answer to the call; inventing an SMS status to keep the
		// list rendering would be worse than saying the read failed.
		return nil, err
	}

	items := make([]dto.AdminTransactionSummaryResponse, 0, len(rows))
	for index := range rows {
		row := &rows[index]
		status, hasStatus := statuses[row.PhoneNumber]
		items = append(items, toAdminSummary(row, status, hasStatus))
	}
	return &dto.AdminTransactionPage{
		Items:      items,
		TotalCount: total,
		Page:       params.Page,
		PageSize:   params.PageSize,
	}, nil
}

// panelParams turns the panel's query into the repository's own params, which is
// where every rule about a searchable value lives. The transport's binding tags are
// one caller's gate, not the boundary.
func panelParams(filter *dto.AdminTransactionFilter) (repository.PanelListParams, error) {
	params := repository.PanelListParams{Page: 1, PageSize: defaultPanelPageSize}
	if filter == nil {
		return params, nil
	}

	if filter.Page > 0 {
		params.Page = filter.Page
	}
	if filter.PageSize > 0 {
		params.PageSize = filter.PageSize
	}
	if params.PageSize > maxPanelPageSize {
		params.PageSize = maxPanelPageSize
	}

	state, err := panelState(filter.State)
	if err != nil {
		return params, err
	}
	params.State = state

	phoneNumber, err := panelPhoneNumber(filter.PhoneNumber)
	if err != nil {
		return params, err
	}
	params.PhoneNumber = phoneNumber
	params.PackageID = filter.PackageID

	window, err := panelWindowOf(filter.From, filter.To)
	if err != nil {
		return params, err
	}
	params.From = window.from
	params.To = window.to
	return params, nil
}

// panelState keeps one state only when the lifecycle knows it. An unknown value is
// a typo, and answering it with an empty page would read as "no such Transaction".
func panelState(raw string) (*entity.State, error) {
	if raw == "" {
		return nil, nil
	}

	state := entity.State(raw)
	if !state.Known() {
		return nil, &FilterError{Field: "state", Reason: "State transaksi tidak dikenal."}
	}
	return &state, nil
}

// panelPhoneNumber canonicalizes the input because the panel may receive the number
// in the form the buyer typed it; a search that only matched the canonical form
// would answer "no results" for a real customer. An input that is not a Phone Number
// cannot match any row.
func panelPhoneNumber(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	normalized, err := phone.Normalize(raw)
	if err != nil {
		return "", &FilterError{Field: "phone_number", Reason: "Phone Number tidak valid."}
	}
	return normalized, nil
}

// panelWindow is the half-open time window a search asks for.
type panelWindow struct {
	from *time.Time
	to   *time.Time
}

// panelWindowOf parses the two day bounds. The input names a day an operator wants
// included, so the query stops at the first instant of the day after it.
func panelWindowOf(rawFrom, rawTo string) (panelWindow, error) {
	from, err := panelDay(rawFrom, "from")
	if err != nil {
		return panelWindow{}, err
	}

	to, err := panelDay(rawTo, "to")
	if err != nil {
		return panelWindow{}, err
	}
	if to == nil {
		return panelWindow{from: from}, nil
	}

	end := to.AddDate(0, 0, 1)
	if from != nil && !end.After(*from) {
		return panelWindow{}, &FilterError{
			Field:  "from",
			Reason: "Rentang tanggal terbalik: isi tanggal awal sebelum tanggal akhir.",
		}
	}
	return panelWindow{from: from, to: &end}, nil
}

// panelDay parses one optional day bound. An absent bound is nil, which the
// repository reads as "no bound on this side".
func panelDay(raw string, field string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.ParseInLocation(panelDateLayout, raw, panelDayLocation)
	if err != nil {
		return nil, &FilterError{
			Field:  field,
			Reason: "Tanggal harus berbentuk YYYY-MM-DD.",
		}
	}
	return &parsed, nil
}

// panelPhoneNumbers collects the page's Phone Numbers for the one status read.
func panelPhoneNumbers(rows []entity.Transaction) []string {
	numbers := make([]string, 0, len(rows))
	for index := range rows {
		numbers = append(numbers, rows[index].PhoneNumber)
	}
	return numbers
}
