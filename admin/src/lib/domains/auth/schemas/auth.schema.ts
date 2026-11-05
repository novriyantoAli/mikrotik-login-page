import { z } from 'zod';

/**
 * The panel's own identity contract (ADR 0024). Field names match the Go DTOs
 * in `backend/internal/application/admin/dto` exactly; the shared fixtures in
 * `contracts/` are the single source both sides are tested against.
 */

/** The three roles the server's permission matrix knows. */
export const ADMIN_ROLES = ['owner', 'support', 'auditor'] as const;
export const AdminRoleSchema = z.enum(ADMIN_ROLES);
export type AdminRole = z.infer<typeof AdminRoleSchema>;

export const roleLabels: Record<AdminRole, string> = {
  owner: 'Owner',
  support: 'Support',
  auditor: 'Auditor',
};

/** What an operator reads next to a role, so the matrix is not folklore. */
export const roleDescriptions: Record<AdminRole, string> = {
  owner: 'Mengelola katalog, admin, dan membuka PIN.',
  support: 'Membaca katalog dan daftar, membuka PIN.',
  auditor: 'Membaca katalog, daftar, dan audit log.',
};

const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export const EmailSchema = z
  .string()
  .trim()
  .min(1, 'Email wajib diisi.')
  .refine((value) => emailPattern.test(value), 'Email tidak valid.');

/** The server hashes with bcrypt; 72 bytes is its hard input limit. */
export const MAX_PASSWORD_BYTES = 72;
export const MIN_PASSWORD_LENGTH = 12;

export const AdminPasswordSchema = z
  .string()
  .min(MIN_PASSWORD_LENGTH, 'Kata sandi minimal 12 karakter.')
  .refine(
    (value) => new TextEncoder().encode(value).length <= MAX_PASSWORD_BYTES,
    'Kata sandi maksimal 72 byte.',
  );

/** Sign-in: the server answers 401 for both an unknown email and a wrong password. */
export const LoginInputSchema = z.object({
  email: EmailSchema,
  password: z.string().min(1, 'Kata sandi wajib diisi.'),
});
export type LoginInput = z.infer<typeof LoginInputSchema>;

/** The identity behind the admin cookie. There is deliberately no token field. */
export const SessionSchema = z
  .object({
    email: z.string().min(1),
    role: AdminRoleSchema,
    expires_at: z.string().min(1),
  })
  .strict();
export type Session = z.infer<typeof SessionSchema>;

export const SessionEnvelopeSchema = z.object({ data: SessionSchema }).strict();

export const ChangePasswordInputSchema = z
  .object({
    current_password: z.string().min(1, 'Kata sandi saat ini wajib diisi.'),
    new_password: AdminPasswordSchema,
  })
  .strict();
export type ChangePasswordInput = z.infer<typeof ChangePasswordInputSchema>;

/** Indonesian copy for the two session states the shell can show. */
export const sessionExpiredMessage = 'Sesi Anda sudah berakhir. Masuk lagi untuk melanjutkan.';
export const signedOutMessage = 'Anda sudah keluar dari panel.';
