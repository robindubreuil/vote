// @vitest-environment node
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const template = readFileSync(join(__dirname, '..', 'scripts', 'sw.template.js'), 'utf8')

// FH5 regression guard: the service worker must only intercept HTML
// navigations whose pathname starts with `/formateur/`. Stagiaire is
// intentionally NOT a PWA, so a trainee reloading `/stagiaire/` offline
// should see the browser's native offline behaviour, not the cached
// formateur shell. Scope-narrowing via register({scope}) would have the
// same effect for new clients but break the upgrade path for users who
// already have the SW at scope `/`; gating in the fetch handler fixes
// both populations on the next SW update.
describe('service worker navigation scope (FH5)', () => {
  it('defines the formateur navigation prefix', () => {
    expect(template).toMatch(/FORMATEUR_NAV_PREFIX\s*=\s*['"]\/formateur\/['"]/)
  })

  it('skips non-formateur navigations before responding', () => {
    // The fetch handler must early-return on navigations whose pathname
    // is not under /formateur/, so the browser handles them normally.
    const navigateBlock = template.match(/if\s*\(\s*req\.mode\s*===\s*['"]navigate['"]\s*\)\s*\{[^}]*\}/s)
    expect(navigateBlock, 'navigate handler block not found').not.toBeNull()
    expect(navigateBlock[0]).toMatch(/startsWith\(FORMATEUR_NAV_PREFIX\)/)
    expect(navigateBlock[0]).toMatch(/\breturn\b/)
  })
})
