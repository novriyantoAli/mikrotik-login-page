import { z } from 'zod';

/**
 * The panel's Package catalog contract (ADR 0025). It is deliberately not the
 * frozen public catalog: the panel edits typed values (integer rupiah, minutes,
 * the MikroTik rate limit) and must see retired Packages and each row's
 * version. Names and bounds mirror
 * `backend/internal/application/packages/service/validation.go`.
 */

export const MAX_PACKAGE_NAME_LENGTH = 80;
export const MAX_PACKAGE_PRICE_IDR = 100_000_000;
export const MAX_PACKAGE_DURATION_MINUTES = 527_040;

const codePattern = /^[a-z0-9]+(-[a-z0-9]+)*$/;
const rateLimitPattern = /^\d+(\.\d+)?[kKmMgG]?\/\d+(\.\d+)?[kKmMgG]?$/;

/** One row of GET /api/admin/packages. */
export const AdminPackageSchema = z
  .object({
    id: z.number().int().positive(),
    code: z.string().min(1),
    name: z.string().min(1),
    price_idr: z.number().int().nonnegative(),
    duration_minutes: z.number().int().positive(),
    rate_limit: z.string().min(1),
    is_active: z.boolean(),
    version: z.number().int().positive(),
    created_at: z.string().min(1),
    updated_at: z.string().min(1),
  })
  .strict();
export type AdminPackage = z.infer<typeof AdminPackageSchema>;

export const AdminPackageListEnvelopeSchema = z
  .object({ data: z.array(AdminPackageSchema) })
  .strict();

export const AdminPackageEnvelopeSchema = z.object({ data: AdminPackageSchema }).strict();

const priceMessage = `Harga harus bilangan bulat lebih dari 0 dan paling banyak Rp ${MAX_PACKAGE_PRICE_IDR.toLocaleString('id-ID')}.`;

const PackageFieldsSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, 'Nama paket wajib diisi.')
    .max(MAX_PACKAGE_NAME_LENGTH, `Nama paket paling banyak ${MAX_PACKAGE_NAME_LENGTH} karakter.`)
    // eslint-disable-next-line no-control-regex
    .refine((value) => !/[\u0000-\u001f\u007f]/.test(value), {
      message: 'Nama paket tidak boleh memuat karakter kontrol.',
    }),
  price_idr: z.coerce
    .number({ error: 'Harga wajib diisi dengan angka.' })
    .int('Harga harus bilangan bulat.')
    .gt(0, priceMessage)
    .max(MAX_PACKAGE_PRICE_IDR, priceMessage),
  duration_minutes: z.coerce
    .number({ error: 'Durasi wajib diisi dengan angka.' })
    .int('Durasi harus bilangan bulat.')
    .gt(
      0,
      `Durasi harus lebih dari 0 dan paling banyak ${MAX_PACKAGE_DURATION_MINUTES} menit (366 hari).`,
    )
    .max(
      MAX_PACKAGE_DURATION_MINUTES,
      `Durasi harus lebih dari 0 dan paling banyak ${MAX_PACKAGE_DURATION_MINUTES} menit (366 hari).`,
    ),
  rate_limit: z
    .string()
    .trim()
    .min(1, 'Batas kecepatan wajib diisi.')
    .regex(rateLimitPattern, 'Batas kecepatan harus berbentuk unduh/unggah seperti 5M/5M.'),
});

/** A new Package. `code` is the seed's identity and immutable afterwards. */
export const CreatePackageInputSchema = PackageFieldsSchema.extend({
  code: z
    .string()
    .trim()
    .min(1, 'Code paket wajib diisi.')
    .regex(
      codePattern,
      'Code paket hanya boleh huruf kecil, angka, dan tanda hubung (contoh: paket-harian).',
    ),
}).strict();
export type CreatePackageInput = z.infer<typeof CreatePackageInputSchema>;

/**
 * An edit of a Package the panel already read. `version` must be the one that
 * was read, so a price nobody saw cannot be overwritten; `code` is echoed only
 * so an attempt to change it is refused by the server.
 */
export const UpdatePackageInputSchema = PackageFieldsSchema.extend({
  code: z.string().min(1),
  version: z.number().int().positive(),
}).strict();
export type UpdatePackageInput = z.infer<typeof UpdatePackageInputSchema>;

export const SetActiveInputSchema = z.object({ active: z.boolean() }).strict();
export type SetActiveInput = z.infer<typeof SetActiveInputSchema>;

/** The stale-edit 409 body: the shared envelope plus the row as it is now. */
export const VersionConflictSchema = z
  .object({
    error: z
      .object({
        code: z.string(),
        message: z.string(),
        current: AdminPackageSchema,
      })
      .strict(),
  })
  .strict();
export type VersionConflict = z.infer<typeof VersionConflictSchema>;

/**
 * Read a 409 body when the server sent one. Returns null for any other failure,
 * so the caller can fall back to the generic error path.
 */
export function readVersionConflict(data: unknown): VersionConflict['error'] | null {
  const parsed = VersionConflictSchema.safeParse(data);
  return parsed.success ? parsed.data.error : null;
}

/** Human copy for a Package row, reused by the table and the form. */
export function formatPrice(priceIDR: number): string {
  return new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR' }).format(priceIDR);
}

export function formatDuration(minutes: number): string {
  if (minutes % 1440 === 0) {
    const days = minutes / 1440;
    return `${days} hari`;
  }
  if (minutes % 60 === 0) {
    const hours = minutes / 60;
    return `${hours} jam`;
  }
  return `${minutes} menit`;
}
