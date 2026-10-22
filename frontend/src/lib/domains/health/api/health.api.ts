import { AppError, apiClient, isAppError } from '$lib/api/client';
import { HealthEnvelopeSchema, type Health } from '../schemas/health.schema';

/**
 * Read the backend health report.
 *
 * A degraded backend answers 503 with a perfectly valid report, and that report
 * is exactly what the status screen must render — so a 503 is not an error here,
 * it is data. Only an unreachable or unparseable backend is a failure.
 */
export interface HealthApi {
  get(): Promise<Health>;
}

export const healthApi: HealthApi = {
  async get(): Promise<Health> {
    try {
      const response = await apiClient.get('/health');
      return HealthEnvelopeSchema.parse(response.data).data;
    } catch (error) {
      // The response interceptor has already normalised the failure, so the
      // body of a 503 lives on the AppError rather than on an AxiosError.
      if (isAppError(error)) {
        if (error.data !== undefined) {
          const parsed = HealthEnvelopeSchema.safeParse(error.data);
          if (parsed.success) {
            return parsed.data.data;
          }
        }

        // A request that never reached the server has no HTTP status, and its
        // message is already the useful one.
        if (error.status === undefined) {
          throw error;
        }
      }

      // Anything else (a 502 HTML page, a zod failure on a drifted contract) is
      // reported as one Indonesian message instead of a raw parse error.
      throw new AppError('Laporan status dari server tidak dikenali. Periksa lagi nanti.', {
        status: isAppError(error) ? error.status : undefined,
      });
    }
  },
};
