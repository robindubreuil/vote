// @vitest-environment node
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const mainSrc = readFileSync(join(__dirname, 'main.js'), 'utf8')
const handlersSrc = readFileSync(join(__dirname, 'handlers.js'), 'utf8')

// F15 regression guard. The prenom used to live in localStorage, which
// is device-scoped and survives a tab close. That clashed with S6/S12:
// identity (stagiaireId + reclaim token + session code) was carefully
// scoped to sessionStorage (tab-scoped), but the prenom leaked across
// users on a shared tablet and auto-joined the next visitor under the
// previous user's name. The fix moves the prenom to sessionStorage so
// it shares the same boundary as the rest of the identity bundle.
//
// These tests read the source rather than executing init() because
// main.js auto-runs at import time.
describe('stagiaire prenom storage boundary (F15)', () => {
  it('main.js reads the prenom via safeSessionGet, not safeLocalGet', () => {
    expect(mainSrc).toMatch(/safeSessionGet\(['"]vote_stagiaire_prenom['"]/)
    expect(mainSrc).not.toMatch(/safeLocalGet\(['"]vote_stagiaire_prenom['"]/)
  })

  it('main.js no longer imports safeLocalGet', () => {
    // The previous import was `import { safeLocalGet, safeSessionGet }`.
    // After the fix, only safeSessionGet is needed.
    expect(mainSrc).not.toMatch(/safeLocalGet/)
  })

  it('handlers.js writes the prenom via safeSessionSet, not safeLocalSet', () => {
    // Both handleJoin and handleEditName call safeSessionSet with the
    // prenom key. Verify at least one call site exists and no
    // safeLocalSet call references the prenom key.
    expect(handlersSrc).toMatch(/safeSessionSet\(['"]vote_stagiaire_prenom['"]/)
    expect(handlersSrc).not.toMatch(/safeLocalSet\(['"]vote_stagiaire_prenom['"]/)
  })
})
