package service

import "time"

// SweepCadence is how often one worker sweep of this domain runs and, when the
// sweep asks an external system about a row at a time, how many rows one run
// takes. Batch is 0 for a sweep that is bounded by its own statement instead.
type SweepCadence struct {
	// Interval is the longest the sweep is allowed to stay quiet.
	Interval time.Duration
	// Batch bounds one run so a backlog is chipped away instead of holding the
	// worker (or one gateway round trip per row) for minutes.
	Batch int
}

// SweepSettings are the three Transaction sweeps' cadences in one value. They are
// one concept — how the worker paces this domain — read from three config keys, so
// they are provided once instead of as three near-identical structs in two
// packages (ADR 0018).
type SweepSettings struct {
	// Expire closes overdue pending Transactions.
	Expire SweepCadence
	// Provision turns paid Transactions into hotspot accounts.
	Provision SweepCadence
	// Reconcile asks the gateway about pending Transactions still payable.
	Reconcile SweepCadence
}
