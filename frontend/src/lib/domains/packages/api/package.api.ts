import { apiClient } from '$lib/api/client';

import { PackageListEnvelopeSchema, type Package } from '../schemas/package.schema';

/**
 * The catalog's HTTP surface. `queries/` depends on this interface so a test can
 * swap the implementation without touching a component (frontend skill §3).
 */
export interface PackageApi {
  list(): Promise<Package[]>;
}

export const packageApi: PackageApi = {
  async list(): Promise<Package[]> {
    const { data } = await apiClient.get('/packages');
    return PackageListEnvelopeSchema.parse(data).data;
  },
};
