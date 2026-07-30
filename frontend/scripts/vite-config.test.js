// @vitest-environment node
import { describe, it, expect } from 'vitest'
import { readFileSync, existsSync } from 'node:fs'
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

// F21: a defense-in-depth CSP is injected into the built HTML by the
// cspMeta() plugin. These tests pin the policy shape so a future edit
// can't accidentally widen it (e.g. adding 'unsafe-inline' to
// script-src, or dropping object-src 'none'). They read the policy
// straight from vite.config.js rather than from a built artifact so
// they run in the normal `npm test` flow without a prerequisite build.
describe('vite CSP policy (F21)', () => {
  it("defines a cspMeta plugin that applies only to 'build'", () => {
    expect(config).toMatch(/name:\s*['"]vote-csp-meta['"]/)
    expect(config).toMatch(/apply:\s*['"]build['"]/)
  })

  it("injects the CSP via transformIndexHtml", () => {
    expect(config).toMatch(/transformIndexHtml/)
  })

  it("locks script-src to 'self' (no inline scripts)", () => {
    expect(config).toMatch(/script-src 'self'/)
    // Guard against a future widening: 'unsafe-inline' on script-src
    // would defeat the entire CSP since the threat model is XSS.
    const scriptDirective = config.match(/script-src[^;]*?['"]/)
    expect(scriptDirective).toBeTruthy()
    expect(scriptDirective[0]).not.toContain('unsafe-inline')
  })

  it("allows 'unsafe-inline' only for style-src (dynamic background-color etc.)", () => {
    expect(config).toMatch(/style-src 'self' 'unsafe-inline'/)
  })

  it("blocks plugins and form submission off-origin", () => {
    expect(config).toMatch(/object-src 'none'/)
    expect(config).toMatch(/form-action 'self'/)
    expect(config).toMatch(/base-uri 'self'/)
  })

  it("allows WebSocket connections for the live vote transport", () => {
    expect(config).toMatch(/connect-src 'self' ws: wss:/)
  })

  it("allows the inline-SVG favicon via data: URLs", () => {
    expect(config).toMatch(/img-src 'self' data:/)
  })
})

// F21 end-to-end: assert the built dist/ HTML actually carries the CSP
// meta tag. Skipped when dist/ is absent (e.g. fresh checkout before
// first build) so this doesn't fail a clean `npm test` run; a CI build
// step produces dist/ before this would meaningfully assert.
describe('vite CSP injection into built HTML (F21)', () => {
  const distFormateur = join(__dirname, '..', 'dist', 'formateur', 'index.html')
  const distStagiaire = join(__dirname, '..', 'dist', 'stagiaire', 'index.html')
  const built = existsSync(distFormateur) && existsSync(distStagiaire)

  describe.skipIf(!built)('dist/ present', () => {
    for (const [name, path] of [['formateur', distFormateur], ['stagiaire', distStagiaire]]) {
      it(`${name} index.html carries the CSP meta tag`, () => {
        const html = readFileSync(path, 'utf8')
        expect(html).toMatch(/<meta http-equiv="Content-Security-Policy"/)
        expect(html).toMatch(/script-src 'self'/)
        expect(html).not.toMatch(/<script>(?!.*type=)/) // no inline classic script
      })

      it(`${name} index.html has no inline <script> bodies (script-src 'self' is safe)`, () => {
        const html = readFileSync(path, 'utf8')
        // Allow <script type="module" src="..."> and <link rel="modulepreload">
        // but reject <script>...</script> with inline content. The regex
        // matches a <script> tag NOT containing src=, followed by non-tag
        // content and a closing </script>.
        const inlineClassic = html.match(/<script>(?![^<]*<\/script>\s*$)[\s\S]*?<\/script>/)
        const inlineModule = html.match(/<script type="module">[\s\S]*?<\/script>/)
        expect(inlineClassic, 'unexpected inline classic script').toBeNull()
        expect(inlineModule, 'unexpected inline module script').toBeNull()
      })
    }
  })
})
