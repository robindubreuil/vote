// @vitest-environment node
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const pwaSrc = readFileSync(join(__dirname, 'shared', 'pwa.js'), 'utf8')

// F9 + F19: pwa.js hardcoded four French strings ("Une nouvelle version…",
// "Recharger", aria-label="Fermer", "Hors ligne — les votes ne sont
// pas reçus."). Even for a French-only app, `t` is the single point
// for typo fixes. The fix routes the toast text through t.common.*
// keys; these tests pin the contract so a future edit can't silently
// inline the literals again. The module was renamed i18n.js →
// strings.js in F19 (there is no locale system and never was one; the
// old name implied i18n machinery that doesn't exist).
describe('pwa.js strings consolidation (F9 / F19)', () => {
  it('imports t from ./strings.js (renamed from i18n.js in F19)', () => {
    expect(pwaSrc).toMatch(/from\s+['"]\.\/strings\.js['"]/)
    expect(pwaSrc).not.toMatch(/from\s+['"]\.\/i18n\.js['"]/)
  })

  it('renders the update toast text from t.common.pwaUpdateAvailable', () => {
    expect(pwaSrc).toMatch(/t\.common\.pwaUpdateAvailable/)
    expect(pwaSrc).not.toMatch(/Une nouvelle version est disponible/)
  })

  it('renders the reload button from t.common.pwaReload', () => {
    expect(pwaSrc).toMatch(/t\.common\.pwaReload/)
    expect(pwaSrc).not.toMatch(/>\s*Recharger\s*</)
  })

  it('renders the close aria-label from t.common.pwaClose', () => {
    expect(pwaSrc).toMatch(/t\.common\.pwaClose/)
    expect(pwaSrc).not.toMatch(/aria-label="Fermer"/)
  })

  it('renders the offline toast from t.common.pwaOffline', () => {
    expect(pwaSrc).toMatch(/t\.common\.pwaOffline/)
    expect(pwaSrc).not.toMatch(/Hors ligne — les votes ne sont pas reçus/)
  })
})
