import { z } from 'zod';

/**
 * The public Package catalog contract (PRD point 8). Field names are frozen by
 * the Login Page, which renders these display strings verbatim, so the app must
 * not expect typed money or durations here. The shared fixture in
 * `contracts/packages.response.json` is what both sides are tested against.
 */
export const PackageSchema = z
  .object({
    id: z.number().int().positive(),
    nama: z.string().min(1),
    harga: z.string().min(1),
    durasiActivePeriod: z.string().min(1),
    batasKecepatan: z.string().min(1),
  })
  .strict();
export type Package = z.infer<typeof PackageSchema>;

export const PackageListEnvelopeSchema = z.object({ data: z.array(PackageSchema) }).strict();

/** Find the Package the buyer tapped on the Login Page. */
export function findPackage(packages: Package[], id: number): Package | undefined {
  return packages.find((pkg) => pkg.id === id);
}
