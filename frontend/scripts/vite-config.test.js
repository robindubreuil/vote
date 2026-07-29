// @vitest-environment node
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const config = readFileSync(join(__dirname, '..', 'vite.config.js'), 'utf8')

// F13: the build target was Vite's default ("modules" ≈ Chrome 87+),
// which silently dropped support for older Chromium shells still found
// in classroom hardware. Pinning `target: 'es2019'` extends support to
// ~Chrome 80+ and lets esbuild transpile optional chaining and
// nullish coalescing (used throughout app code) down to ES2019.
describe('vite build target (F13)', () => {
  it("pins the build target to 'es2019'", () => {
    expect(config).toMatch(/target:\s*['"]es2019['"]/)
  })
})
