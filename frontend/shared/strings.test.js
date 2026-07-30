import { describe, it, expect } from 'vitest'
import { t } from './strings.js'

// F9: hardcoded French strings bypassed `t` in ~15 sites (pwa.js,
// game aria-labels, 'Anonyme', 'Session expirée...'). Centralising
// them in `t` is only useful if the keys actually exist and the
// call sites use them — these tests pin the contract so a future
// refactor can't silently drop a key.
//
// F19: the module was renamed i18n.js → strings.js. There is no locale
// system and never was one; the i18n name implied machinery (locale
// switcher, pluralization rules, message catalogs) that doesn't exist.
// `strings.js` describes what the module actually is: a single French
// string catalog consumed via the `t` export.
describe('strings keys (F9 / F19)', () => {
  it('exposes the shared anonymous fallback from t.common', () => {
    expect(t.common.anonymous).toBe('Anonyme')
  })

  it('exposes PWA toast strings from t.common', () => {
    expect(t.common.pwaUpdateAvailable).toMatch(/version/)
    expect(t.common.pwaReload).toMatch(/Recharger/)
    expect(t.common.pwaClose).toMatch(/Fermer/)
    expect(t.common.pwaOffline).toMatch(/Hors ligne/)
  })

  it('exposes the game-board aria labels from t.stagiaire', () => {
    // These feed aria-label / <legend> text — centralising them
    // means a screen-reader wording tweak is one edit.
    for (const key of [
      'gameBoardAriaLabel',
      'gamePaletteAriaLabel',
      'gameSlotEmpty',
      'gameSlotFilled',
      'gamePegBlack',
      'gamePegWhite',
      'gamePegBlackDesc',
      'gamePegWhiteDesc',
      'gameRulesOk'
    ]) {
      expect(typeof t.stagiaire[key]).toBe('string')
      expect(t.stagiaire[key].length).toBeGreaterThan(0)
    }
  })

  it('exposes a level-progress title helper that interpolates the next level', () => {
    expect(typeof t.stagiaire.gameLevelProgressTitle).toBe('function')
    expect(t.stagiaire.gameLevelProgressTitle(50, 3)).toMatch(/50/)
    expect(t.stagiaire.gameLevelProgressTitle(50, 3)).toMatch(/Niveau 3/)
  })

  it('exposes a gameLevelMax title for capped players', () => {
    expect(t.stagiaire.gameLevelMax).toMatch(/Niveau/)
  })

  it('exposes the server-sent reclaim-error sentinel under t.stagiaire', () => {
    // This string is matched against the incoming WebSocket error
    // message in stagiaire/websocket.js. Keep in sync with the
    // server's UserFacingError(ErrReclaimUnauthorized) mapping in
    // backend/internal/vote/errors.go.
    expect(t.stagiaire.sessionExpired).toBe('Session expirée — veuillez recréer votre identité')
  })

  it('keeps the existing sessionNotFound / connectionError keys', () => {
    // Sanity: these pre-existing keys are still defined and are now
    // used in places that previously inlined the literals.
    expect(t.stagiaire.sessionNotFound).toBe('Session introuvable')
    expect(t.stagiaire.connectionError).toBe('Erreur de connexion')
  })

  it('does NOT keep a duplicate t.formateur.anonymous (consolidated to common)', () => {
    expect(t.formateur.anonymous).toBeUndefined()
  })
})

// F17: test-only surface is gated behind `import.meta.env.DEV`. Vitest
// runs in dev mode so the live object is visible here; production
// builds (where Vite statically replaces DEV with false) collapse it
// to `null`, which closes the console-callable surface.
describe('test-utils gating (F17)', () => {
  it('exposes _test in dev (vitest sees the live object)', () => {
    // Dynamic import keeps the assertion independent of each module's
    // own top-level side effects.
    return Promise.all([
      import('./game-storage.js'),
      import('./presets.js')
    ]).then(([gs, ps]) => {
      expect(gs._test).toBeTypeOf('object')
      expect(gs._test).not.toBeNull()
      expect(typeof gs._test.resetForTests).toBe('function')
      expect(typeof gs._test.constants).toBe('object')

      expect(ps._test).toBeTypeOf('object')
      expect(ps._test).not.toBeNull()
      expect(typeof ps._test.resetForTests).toBe('function')
      expect(typeof ps._test.constants).toBe('object')
    })
  })

  it('no longer exports the old _resetForTests / _constants names', () => {
    return Promise.all([
      import('./game-storage.js'),
      import('./presets.js')
    ]).then(([gs, ps]) => {
      // The old names were removed from the public surface; the new
      // gated `_test` bag replaces them.
      expect(gs._resetForTests).toBeUndefined()
      expect(gs._constants).toBeUndefined()
      expect(ps._resetForTests).toBeUndefined()
      expect(ps._constants).toBeUndefined()
    })
  })
})
