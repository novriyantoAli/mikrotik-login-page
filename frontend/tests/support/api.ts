import { AxiosError, AxiosHeaders, type AxiosAdapter, type AxiosResponse } from 'axios';

import { apiClient } from '$lib/api/client';

const originalAdapter = apiClient.defaults.adapter;

export interface StubbedResponse {
  status?: number;
  data?: unknown;
}

/**
 * Replace the axios adapter with a canned response. Nothing leaves jsdom: the
 * domain sees exactly what the contract says the backend returns.
 */
export function stubApi(responder: () => StubbedResponse | Promise<StubbedResponse>): void {
  const adapter: AxiosAdapter = async (config) => {
    const result = await responder();
    const response: AxiosResponse = {
      data: result.data,
      status: result.status ?? 200,
      statusText: '',
      headers: new AxiosHeaders(),
      config,
    };

    if (response.status >= 400) {
      throw new AxiosError(
        `Request failed with status code ${response.status}`,
        String(response.status),
        config,
        undefined,
        response,
      );
    }
    return response;
  };

  apiClient.defaults.adapter = adapter;
}

/** Simulate an unreachable backend (DNS/TCP failure, no HTTP response). */
export function stubNetworkFailure(): void {
  const adapter: AxiosAdapter = async (config) => {
    throw new AxiosError('Network Error', 'ERR_NETWORK', config);
  };

  apiClient.defaults.adapter = adapter;
}

export function restoreApi(): void {
  apiClient.defaults.adapter = originalAdapter;
}
