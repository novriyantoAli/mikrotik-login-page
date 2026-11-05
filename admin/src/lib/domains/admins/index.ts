/**
 * Public surface of the admins domain. Other domains and routes import only
 * from here, never from schemas/, api/, queries/ or components/ directly.
 */
export { default as AdminCreateForm } from './components/AdminCreateForm.svelte';
export { default as AdminList } from './components/AdminList.svelte';
export { default as AdminManagement } from './components/AdminManagement.svelte';
export { default as AdminPasswordForm } from './components/AdminPasswordForm.svelte';
export {
  adminKeys,
  createAdminListQuery,
  createAdminMutation,
  createResetAdminPasswordMutation,
  createSetAdminActiveMutation,
} from './queries/admin.queries';
export {
  AdminEnvelopeSchema,
  AdminListEnvelopeSchema,
  AdminSchema,
  CreateAdminInputSchema,
  ResetPasswordInputSchema,
  SetActiveEnvelopeSchema,
  SetActiveInputSchema,
  SetActiveResponseSchema,
} from './schemas/admin.schema';
export type {
  Admin,
  CreateAdminInput,
  ResetPasswordInput,
  SetActiveInput,
  SetActiveResponse,
} from './schemas/admin.schema';
