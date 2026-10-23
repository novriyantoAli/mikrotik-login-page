import { z } from 'zod';

/**
 * The passwordless entry contract (ADR 0011). Field names match the Go DTOs in
 * `backend/internal/application/auth/dto` exactly; the shared fixtures in
 * `contracts/` are the single source both sides are tested against.
 */

/** What the app asks for: a Phone Number, and optionally a checkout target. */
export const MagicLinkRequestSchema = z
  .object({
    phone_number: z.string().min(1, 'Phone Number wajib diisi.'),
    package_id: z.number().int().positive().optional(),
    return_url: z.string().url().optional(),
  })
  .strict();
export type MagicLinkRequest = z.infer<typeof MagicLinkRequestSchema>;

/**
 * The only answer the request endpoint gives. It is identical for every Phone
 * Number, so the app must never treat it as "this number exists".
 */
export const MagicLinkSentSchema = z.object({ sent: z.literal(true) }).strict();
export type MagicLinkSent = z.infer<typeof MagicLinkSentSchema>;

export const MagicLinkSentEnvelopeSchema = z.object({ data: MagicLinkSentSchema }).strict();

/** The identity behind the session cookie. */
export const SessionSchema = z
  .object({
    phone_number: z.string().min(1),
    expires_at: z.string().min(1),
  })
  .strict();
export type Session = z.infer<typeof SessionSchema>;

export const SessionEnvelopeSchema = z.object({ data: SessionSchema }).strict();

/** Why the link was issued, so the app can land the user where they asked. */
export const MagicLinkPurposeSchema = z.enum(['checkout', 'account']);
export type MagicLinkPurpose = z.infer<typeof MagicLinkPurposeSchema>;

/**
 * The session plus the intent stored with the link. There is deliberately no
 * token field: the session token only ever travels in the HttpOnly cookie.
 */
export const ExchangeResultSchema = SessionSchema.extend({
  purpose: MagicLinkPurposeSchema,
  package_id: z.number().int().positive().optional(),
  return_url: z.string().optional(),
}).strict();
export type ExchangeResult = z.infer<typeof ExchangeResultSchema>;

export const ExchangeResultEnvelopeSchema = z.object({ data: ExchangeResultSchema }).strict();

/** Indonesian copy for the two states the redeem screen can end in. */
export const invalidLinkMessage = 'Tautan tidak berlaku. Minta tautan masuk baru.';
export const queuedLinkMessage =
  'Tautan masuk sudah kami kirim lewat SMS. Periksa pesan Anda, lalu buka tautannya.';
