import axios, { type AxiosError } from 'axios';

import { env } from '$lib/config/env';

/**
 * One consistent error shape the whole app can rely on. The backend answers
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

/** The single axios instance for the app. Domains never create their own. */
export const apiClient = axios.create({
  baseURL: env.apiBaseUrl,
  timeout: env.requestTimeoutMs,
  headers: { Accept: 'application/json' },
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
  (error) => Promise.reject(normalizeApiError(error)),
);
