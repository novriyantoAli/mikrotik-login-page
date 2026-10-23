package sms

// TaskDeliver is the wake-up task the worker drainer listens for. The task
// carries no payload: the rows in sms_outbox are the work (ADR 0010).
const TaskDeliver = "sms:deliver"
