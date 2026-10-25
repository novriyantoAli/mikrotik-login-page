import type { DependencyName, FailureCategory } from '../schemas/health.schema';

/**
 * Copy for the status screen, next to the components that render it. It lives
 * here rather than in `schemas/` because a schema is the validation boundary and
 * this is interface text: the two change for different reasons (frontend layer
 * table, §15). `ui-language.test.ts` scans the string literals of every `.ts`
 * module under `src`, so moving the copy here keeps it inside the glossary check
 * instead of escaping it.
 */

/** Indonesian explanation for each failure category. */
export const failureLabels: Record<FailureCategory, string> = {
  timeout: 'Tidak merespons tepat waktu.',
  unreachable: 'Tidak dapat dihubungi.',
  auth_failed: 'Kredensial ditolak server.',
  unavailable: 'Tidak tersedia.',
};

/** Indonesian label for each dependency, used by the status screen. */
export const dependencyLabels: Record<DependencyName, string> = {
  postgres: 'Postgres',
  freeradius_mysql: 'MySQL FreeRADIUS',
  redis: 'Redis',
};
