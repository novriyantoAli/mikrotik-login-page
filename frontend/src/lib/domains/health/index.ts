/**
 * Public surface of the health domain. Other domains and routes import only from
 * here, never from schemas/, api/, queries/ or components/ directly.
 */
export { default as HealthStatusCard } from './components/HealthStatusCard.svelte';
export { createHealthQuery, healthKeys } from './queries/health.queries';
export { healthApi } from './api/health.api';
export {
  HealthSchema,
  HealthEnvelopeSchema,
  dependencyLabels,
  failureLabels,
} from './schemas/health.schema';
export type {
  Health,
  HealthCheck,
  HealthEnvelope,
  DependencyName,
  FailureCategory,
} from './schemas/health.schema';
