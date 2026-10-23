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

export interface StubRoute {
  /** HTTP method, case-insensitive; omitted matches any method. */
  method?: string;
  /** Exact path or a pattern; the path is compared without the baseURL. */
  path: string | RegExp;
  respond: () => StubbedResponse | Promise<StubbedResponse>;
}

function matches(route: StubRoute, method: string, url: string): boolean {
  if (route.method && route.method.toLowerCase() !== method) {
    return false;
  }
  return typeof route.path === 'string' ? route.path === url : route.path.test(url);
}

/**
 * Replace the adapter with a tiny router. A checkout screen calls several
 * endpoints, and each test must answer them independently.
 */
export function stubApiRoutes(routes: StubRoute[]): void {
  const adapter: AxiosAdapter = async (config) => {
    const method = (config.method ?? 'get').toLowerCase();
    const url = config.url ?? '';
    const route = routes.find((candidate) => matches(candidate, method, url));

    const result: StubbedResponse = route
      ? await route.respond()
      : {
          status: 404,
          data: {
            error: { code: 'not_found', message: `no stub for ${method.toUpperCase()} ${url}` },
          },
        };

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
