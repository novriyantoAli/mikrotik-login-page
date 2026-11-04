/**
 * Public surface of the auth domain. Other domains and routes import only from
 * here, never from schemas/, api/, queries/ or components/ directly.
 */
export { default as ChangePasswordForm } from './components/ChangePasswordForm.svelte';
export { default as LoginForm } from './components/LoginForm.svelte';
export {
  authKeys,
  createChangePasswordMutation,
  createLoginMutation,
  createLogoutMutation,
  createSessionQuery,
} from './queries/auth.queries';
export { authApi, watchSessionExpiry } from './api/auth.api';
export {
  ADMIN_ROLES,
  AdminPasswordSchema,
  AdminRoleSchema,
  ChangePasswordInputSchema,
  EmailSchema,
  LoginInputSchema,
  MAX_PASSWORD_BYTES,
  MIN_PASSWORD_LENGTH,
  SessionEnvelopeSchema,
  SessionSchema,
  roleDescriptions,
  roleLabels,
  sessionExpiredMessage,
  signedOutMessage,
} from './schemas/auth.schema';
export type { AdminRole, ChangePasswordInput, LoginInput, Session } from './schemas/auth.schema';
