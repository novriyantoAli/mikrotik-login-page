import { apiClient, isAppError, onUnauthorized, type AppError } from '$lib/api/client';

import {
  ChangePasswordInputSchema,
  LoginInputSchema,
  SessionEnvelopeSchema,
  type ChangePasswordInput,
  type LoginInput,
  type Session,
} from '../schemas/auth.schema';

/**
 * The auth domain's HTTP surface. `queries/` depends on this interface, so a
 * test or a story can swap the implementation without touching components.
 */
export interface AuthApi {
  login(input: LoginInput): Promise<Session>;
  /** The live session, or null when there is none (a 401 is an answer, not an error). */
  session(): Promise<Session | null>;
  logout(): Promise<void>;
  changePassword(input: ChangePasswordInput): Promise<void>;
}

export const authApi: AuthApi = {
  async login(input: LoginInput): Promise<Session> {
    const payload = LoginInputSchema.parse(input);
    const { data } = await apiClient.post('/admin/auth/login', payload);
    return SessionEnvelopeSchema.parse(data).data;
  },

  async session(): Promise<Session | null> {
    try {
      const { data } = await apiClient.get('/admin/auth/session');
      return SessionEnvelopeSchema.parse(data).data;
    } catch (error) {
      // No session is a normal state, not a failure: the login form exists for
      // exactly this case.
      if (isAppError(error) && error.status === 401) {
        return null;
      }
      throw error;
    }
  },

  async logout(): Promise<void> {
    await apiClient.post('/admin/auth/logout');
  },

  async changePassword(input: ChangePasswordInput): Promise<void> {
    const payload = ChangePasswordInputSchema.parse(input);
    await apiClient.post('/admin/auth/password', payload);
  },
};

/**
 * Tell the caller when a request that needed the session came back 401, so the
 * shell can leave the dead screen and show the login form with a message. The
 * prompt and probe routes are excluded by the client itself.
 */
export function watchSessionExpiry(handler: (error: AppError) => void): () => void {
  return onUnauthorized(handler);
}
