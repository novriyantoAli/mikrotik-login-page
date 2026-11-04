import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const contractsDir = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../../contracts',
);

/**
 * Read one shared contract fixture. The same file is decoded by the backend
 * integration tests, so both sides are held to one definition.
 */
export function loadContract<T = unknown>(name: string): T {
  const raw = readFileSync(path.join(contractsDir, name), 'utf8');
  return JSON.parse(raw) as T;
}
