import { apiClient, isAppError } from '$lib/api/client';

import {
  ExchangeResultEnvelopeSchema,
  MagicLinkRequestSchema,
  MagicLinkSentEnvelopeSchema,
  SessionEnvelopeSchema,
  type ExchangeResult,
  type MagicLinkRequest,
  type MagicLinkSent,
  type Session,
} from '../schemas/auth.schema';

/**
 * The auth domain's HTTP surface. `queries/` depends on this interface, so a
 * test or a story can swap the implementation without touching components.
 */
export interface AuthApi {
  requestMagicLink(input: MagicLinkRequest): Promise<MagicLinkSent>;
  exchange(token: string): Promise<ExchangeResult>;
  /** The live session, or null when there is none (a 401 is an answer, not an error). */
  session(): Promise<Session | null>;
  signOut(): Promise<void>;
}

export const authApi: AuthApi = {
  async requestMagicLink(input: MagicLinkRequest): Promise<MagicLinkSent> {
    const payload = MagicLinkRequestSchema.parse(input);
    const { data } = await apiClient.post('/auth/magic-link', payload);
    return MagicLinkSentEnvelopeSchema.parse(data).data;
  },

  async exchange(token: string): Promise<ExchangeResult> {
    const { data } = await apiClient.post('/auth/magic-link/exchange', { token });
    return ExchangeResultEnvelopeSchema.parse(data).data;
  },

  async session(): Promise<Session | null> {
    try {
      const { data } = await apiClient.get('/auth/session');
      return SessionEnvelopeSchema.parse(data).data;
    } catch (error) {
      // No session is a normal state, not a failure: the login screen exists for
      // exactly this case.
      if (isAppError(error) && error.status === 401) {
        return null;
      }
      throw error;
    }
  },

  async signOut(): Promise<void> {
    await apiClient.post('/auth/logout');
  },
};

/**
 * Whether a failed redemption is worth retrying. A used, expired or unknown
 * token answers 400, and retrying it can never succeed — only a transport
 * failure (no status) or a server fault is retriable.
 */
export function isRetriableAuthError(error: unknown): boolean {
  if (!isAppError(error)) {
    return true;
  }
  return error.status === undefined || error.status >= 500;
}
