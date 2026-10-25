import { z } from 'zod';

/**
 * Dependency names as the backend reports them (see
 * `backend/internal/pkg/database/checkers.go`). They are part of the contract:
 * the shared fixture in `contracts/health.response.json` uses the same keys.
 */
export const DependencyNameSchema = z.enum(['postgres', 'freeradius_mysql', 'redis']);
export type DependencyName = z.infer<typeof DependencyNameSchema>;

/**
 * Stable failure categories the backend publishes instead of driver messages
 * (see `backend/internal/application/health/dto/health.dto.go`).
 */
export const FailureCategorySchema = z.enum([
  'timeout',
  'unreachable',
  'auth_failed',
  'unavailable',
]);
export type FailureCategory = z.infer<typeof FailureCategorySchema>;

const latency = z.number().int().nonnegative();

/** A down check must say why, and only in the agreed vocabulary. */
export const HealthCheckSchema = z.discriminatedUnion('status', [
  z.object({ status: z.literal('up'), latency_ms: latency }).strict(),
  z
    .object({ status: z.literal('down'), latency_ms: latency, error: FailureCategorySchema })
    .strict(),
]);

export type HealthCheck = z.infer<typeof HealthCheckSchema>;

export const HealthSchema = z
  .object({
    status: z.enum(['ok', 'degraded']),
    service: z.string().min(1),
    version: z.string().min(1),
    checked_at: z.string().min(1),
    checks: z
      .object({
        postgres: HealthCheckSchema,
        freeradius_mysql: HealthCheckSchema,
        redis: HealthCheckSchema,
      })
      .strict(),
  })
  .strict();

export type Health = z.infer<typeof HealthSchema>;

/** The shared `{ data, ... }` success envelope. */
export const HealthEnvelopeSchema = z.object({ data: HealthSchema }).strict();

export type HealthEnvelope = z.infer<typeof HealthEnvelopeSchema>;
