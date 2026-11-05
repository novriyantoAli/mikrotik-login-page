/**
 * Public surface of the packages domain. Other domains and routes import only
 * from here, never from schemas/, api/, queries/ or components/ directly.
 */
export { default as PackageForm } from './components/PackageForm.svelte';
export { default as PackageList } from './components/PackageList.svelte';
export { default as PackageManagement } from './components/PackageManagement.svelte';
export { versionConflictOf } from './api/package.api';
export type { PackageVersionConflict } from './api/package.api';
export {
  createAdminPackageListQuery,
  createPackageMutation,
  createSetActivePackageMutation,
  createUpdatePackageMutation,
  packageKeys,
} from './queries/package.queries';
export {
  AdminPackageEnvelopeSchema,
  AdminPackageListEnvelopeSchema,
  AdminPackageSchema,
  CreatePackageInputSchema,
  MAX_PACKAGE_DURATION_MINUTES,
  MAX_PACKAGE_NAME_LENGTH,
  MAX_PACKAGE_PRICE_IDR,
  SetActiveInputSchema,
  UpdatePackageInputSchema,
  VersionConflictSchema,
  formatDuration,
  formatPrice,
  readVersionConflict,
} from './schemas/package.schema';
export type {
  AdminPackage,
  CreatePackageInput,
  SetActiveInput,
  UpdatePackageInput,
  VersionConflict,
} from './schemas/package.schema';
