import axios, { type AxiosError } from 'axios';

import { env } from '$lib/config/env';

/**
 * One consistent error shape the whole panel can rely on. The backend answers
 * failures with `{ error: { code, message } }`, so that message is preferred;
 * anything else (network down, HTML error page) gets a generic Indonesian one.
 */
export class AppError extends Error {
  readonly status?: number;
  readonly code?: string;
  /** Raw response body, kept so callers can recognise a meaningful non-2xx payload. */
  readonly data?: unknown;

  constructor(message: string, options: { status?: number; code?: string; data?: unknown } = {}) {
    super(message);
    this.name = 'AppError';
    this.status = options.status;
    this.code = options.code;
    this.data = options.data;
  }
}

export function isAppError(value: unknown): value is AppError {
  return value instanceof AppError;
}

/**
 * Listeners told when a request that needed the admin session came back 401.
 * The panel uses this to leave whatever screen it was on and return to the
 * login form with a message, instead of showing an empty page (ADR 0028).
 */
export type UnauthorizedListener = (error: AppError) => void;

const unauthorizedListeners = new Set<UnauthorizedListener>();

/** Register a listener; the returned function unsubscribes it. */
export function onUnauthorized(listener: UnauthorizedListener): () => void {
  unauthorizedListeners.add(listener);
  return () => {
    unauthorizedListeners.delete(listener);
  };
}

/**
 * The sign-in and session-probe routes answer 401 as a normal part of their
 * job (wrong password, no session yet), so only *other* routes mean "the
 * session you were using is gone".
 */
function isSessionExpiry(url: string | undefined): boolean {
  if (!url) {
    return true;
  }
  return !url.endsWith('/admin/auth/login') && !url.endsWith('/admin/auth/session');
}

function notifyUnauthorized(error: AppError, url: string | undefined): void {
  if (error.status !== 401 || !isSessionExpiry(url)) {
    return;
  }
  for (const listener of unauthorizedListeners) {
    listener(error);
  }
}

/** The single axios instance for the panel. Domains never create their own. */
export const apiClient = axios.create({
  baseURL: env.apiBaseUrl,
  timeout: env.requestTimeoutMs,
  headers: { Accept: 'application/json' },
  // The admin session is an HttpOnly cookie scoped to /api/admin; the browser
  // attaches it on its own, so no token ever reaches JavaScript (ADR 0024).
  withCredentials: true,
});

export function normalizeApiError(error: unknown): AppError {
  if (axios.isAxiosError(error)) {
    const axiosError = error as AxiosError<{ error?: { code?: string; message?: string } }>;
    const payload = axiosError.response?.data?.error;

    if (payload?.message) {
      return new AppError(payload.message, {
        status: axiosError.response?.status,
        code: payload.code,
        data: axiosError.response?.data,
      });
    }

    return new AppError('Tidak dapat menghubungi server. Periksa koneksi Anda lalu coba lagi.', {
      status: axiosError.response?.status,
      data: axiosError.response?.data,
    });
  }

  return new AppError('Terjadi kesalahan yang tidak terduga.');
}

apiClient.interceptors.response.use(
  (response) => response,
  (error) => {
    const normalized = normalizeApiError(error);
    notifyUnauthorized(normalized, axios.isAxiosError(error) ? error.config?.url : undefined);
    return Promise.reject(normalized);
  },
);
