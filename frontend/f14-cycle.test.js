// @vitest-environment node
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const utilsSrc = readFileSync(join(__dirname, 'src', 'formateur', 'utils.js'), 'utf8')
const renderersSrc = readFileSync(join(__dirname, 'src', 'formateur', 'renderers.js'), 'utf8')
const voteDataSrc = readFileSync(join(__dirname, 'src', 'formateur', 'vote-data.js'), 'utf8')

// F14: utils.js used to dynamically `import('./renderers.js')` on every
// vote_received to refresh the combinations and per-stagiaire lists.
// renderers.js in turn statically imported the data helpers from
// utils.js, so the dynamic import was the cycle breaker — but Vite
// warned it was "ineffective" (renderers.js was statically imported
// elsewhere too, so it never split into its own chunk) and a failed
// load would silently stall the panel.
//
// The fix extracts the pure data helpers into vote-data.js, breaking
// the cycle so utils.js can statically import the render fragments.
describe('formateur utils/renderers cycle broken (F14)', () => {
  it('utils.js statically imports render fragments from renderers.js', () => {
    expect(utilsSrc).toMatch(/import\s+\{[^}]*renderCombinationsHTML[^}]*renderStagiairesVotesHTML[^}]*\}\s+from\s+['"]\.\/renderers\.js['"]/)
  })

  it('utils.js no longer dynamic-imports renderers.js', () => {
    // The dynamic import was the smell. If it ever comes back, this
    // catches it (a comment mentioning "dynamic import" is fine —
    // the assertion targets actual import() calls).
    expect(utilsSrc).not.toMatch(/\bimport\(\s*['"]\.\/renderers\.js['"]\s*\)/)
  })

  it('utils.js no longer references guardDynamicImport', () => {
    // The guard was only there to wrap the dynamic import. Once the
    // import is static, the guard is dead weight.
    expect(utilsSrc).not.toMatch(/guardDynamicImport/)
  })

  it('renderers.js imports data helpers from vote-data.js (not utils.js)', () => {
    expect(renderersSrc).toMatch(/from\s+['"]\.\/vote-data\.js['"]/)
    expect(renderersSrc).not.toMatch(/from\s+['"]\.\/utils\.js['"]/)
  })

  it('vote-data.js exists and exports the three data helpers', () => {
    for (const fn of ['getColorCounts', 'getCombinations', 'sortStagiaires']) {
      expect(voteDataSrc).toMatch(new RegExp(`export function ${fn}\\b`))
    }
  })

  it('vote-data.js is pure (no document references)', () => {
    // The whole point of the extraction is that the helpers only read
    // `state` — no DOM, no rendering. If a future edit adds a document
    // touch, it should go through utils.js or renderers.js instead.
    expect(voteDataSrc).not.toMatch(/\bdocument\./)
  })
})
