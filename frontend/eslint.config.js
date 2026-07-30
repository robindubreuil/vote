import js from '@eslint/js'
import globals from 'globals'

// Vitest injects these as globals when `test.globals: true` (set in
// vitest.config.js). Production code never sees them, but co-located
// *.test.js files may rely on any of them without an explicit import —
// without this override ESLint's `no-undef` would flag every `describe`,
// `expect`, `vi`, etc. that wasn't imported. Keeping the list explicit
// (rather than pulling in @vitest/eslint-plugin) avoids a new
// dependency for what is purely a no-undef declaration.
const vitestGlobals = {
  describe: 'readonly',
  it: 'readonly',
  test: 'readonly',
  expect: 'readonly',
  vi: 'readonly',
  beforeAll: 'readonly',
  beforeEach: 'readonly',
  afterAll: 'readonly',
  afterEach: 'readonly',
  assert: 'readonly',
  suite: 'readonly',
  onTestFinished: 'readonly',
}

export default [
  js.configs.recommended,
  {
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      globals: {
        ...globals.browser,
      },
    },
    rules: {
      'no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      'no-constant-condition': ['error', { checkLoops: false }],
      'no-empty': ['error', { allowEmptyCatch: true }],
    },
  },
  // F20: co-located test files use Vitest globals (describe, it, vi,
  // beforeEach…) without importing them because vitest.config.js sets
  // `test.globals: true`. This block declares them so `no-undef`
  // doesn't false-positive on a future test that drops the import.
  {
    files: ['**/*.test.js', '**/*.spec.js'],
    languageOptions: {
      globals: {
        ...globals.node,
        ...vitestGlobals,
      },
    },
  },
  {
    ignores: ['**/dist/', '**/node_modules/', 'tests/', 'scripts/'],
  },
]
