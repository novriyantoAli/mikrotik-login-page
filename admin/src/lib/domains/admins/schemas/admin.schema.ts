import { z } from 'zod';

import { AdminPasswordSchema, AdminRoleSchema, EmailSchema } from '$lib/domains/auth';

/**
 * The panel's admin-management contract (ADR 0024 §3). Only an owner sees this
 * surface; the server enforces that, the UI merely hides it. Field names match
 * `backend/internal/application/admin/dto`.
 */

export const AdminSchema = z
  .object({
    id: z.number().int().positive(),
    email: z.string().min(1),
    role: AdminRoleSchema,
    is_active: z.boolean(),
    created_at: z.string().min(1),
  })
  .strict();
export type Admin = z.infer<typeof AdminSchema>;

export const AdminListEnvelopeSchema = z.object({ data: z.array(AdminSchema) }).strict();

export const AdminEnvelopeSchema = z.object({ data: AdminSchema }).strict();

export const CreateAdminInputSchema = z
  .object({
    email: EmailSchema,
    role: AdminRoleSchema,
    password: AdminPasswordSchema,
  })
  .strict();
export type CreateAdminInput = z.infer<typeof CreateAdminInputSchema>;

export const ResetPasswordInputSchema = z.object({ password: AdminPasswordSchema }).strict();
export type ResetPasswordInput = z.infer<typeof ResetPasswordInputSchema>;

export const SetActiveInputSchema = z.object({ active: z.boolean() }).strict();
export type SetActiveInput = z.infer<typeof SetActiveInputSchema>;

export const SetActiveResponseSchema = z
  .object({ id: z.number().int().positive(), active: z.boolean() })
  .strict();
export type SetActiveResponse = z.infer<typeof SetActiveResponseSchema>;

export const SetActiveEnvelopeSchema = z.object({ data: SetActiveResponseSchema }).strict();
