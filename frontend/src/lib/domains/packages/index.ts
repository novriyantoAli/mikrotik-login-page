/**
 * Public surface of the Package catalog domain. Other domains and routes import
 * only from here, never from schemas/, api/ or queries/ directly.
 */
export { default as PackageSummary } from './components/PackageSummary.svelte';
export { createPackageListQuery, packageKeys } from './queries/package.queries';
export { findPackage, PackageListEnvelopeSchema, PackageSchema } from './schemas/package.schema';
export type { Package } from './schemas/package.schema';
