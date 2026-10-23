/**
 * Public surface of the auth domain. Other domains and routes import only from
 * here, never from schemas/, api/, queries/ or components/ directly.
 */
export { default as MagicLinkForm } from './components/MagicLinkForm.svelte';
export { default as MagicLinkRedeem } from './components/MagicLinkRedeem.svelte';
export {
  authKeys,
  createExchangeMagicLinkMutation,
  createRequestMagicLinkMutation,
  createSessionQuery,
  createSignOutMutation,
} from './queries/auth.queries';
export {
  MagicLinkRequestSchema,
  MagicLinkSentSchema,
  SessionSchema,
  ExchangeResultSchema,
  invalidLinkMessage,
  queuedLinkMessage,
} from './schemas/auth.schema';
export type {
  ExchangeResult,
  MagicLinkPurpose,
  MagicLinkRequest,
  MagicLinkSent,
  Session,
} from './schemas/auth.schema';
