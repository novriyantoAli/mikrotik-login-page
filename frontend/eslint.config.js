import js from '@eslint/js';
import prettier from 'eslint-config-prettier';
import svelte from 'eslint-plugin-svelte';
import globals from 'globals';
import tseslint from 'typescript-eslint';

import svelteConfig from './svelte.config.js';

/**
 * Enforces the layering rules the architecture expects (frontend skill §3):
 * components never import axios, and another domain is only reachable through
 * its index.ts barrel.
 */
const layerRules = [
  'error',
  {
    paths: [
      {
        name: 'axios',
        message:
          'Import the domain api/ layer or $lib/api/client instead of importing axios directly.',
      },
    ],
    patterns: [
      {
        group: [
          '$lib/domains/*/api/*',
          '$lib/domains/*/queries/*',
          '$lib/domains/*/schemas/*',
          '$lib/domains/*/state/*',
        ],
        message: 'Import another domain only through its index.ts barrel.',
      },
    ],
  },
];

export default tseslint.config(
  { ignores: ['.svelte-kit/', 'build/', 'node_modules/', 'coverage/'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...svelte.configs['flat/recommended'],
  prettier,
  ...svelte.configs['flat/prettier'],
  {
    languageOptions: {
      globals: { ...globals.browser, ...globals.node },
    },
  },
  {
    files: ['**/*.svelte'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
        svelteConfig,
      },
    },
  },
  {
    files: ['src/**/*.{ts,svelte}'],
    // The single axios instance is the one place allowed to touch axios.
    ignores: ['src/lib/api/client.ts'],
    rules: { 'no-restricted-imports': layerRules },
  },
);
