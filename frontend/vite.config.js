import { resolve } from 'path'
import { defineConfig } from 'vite'

// F21: defense-in-depth Content-Security-Policy, injected at build time
// only. Dev is left unconstrained so HMR, source maps, and Vite's
// injected client/runtime don't fight a tight policy; production ships
// the strict policy baked into the HTML.
//
// Why a plugin and not a literal <meta> in index.html: keeping the CSP
// in one place (this file) means both entry points (formateur +
// stagiaire) cannot drift, and the policy is grep-able / testable as a
// single source of truth. transformIndexHtml runs once per entry HTML.
//
// Directives:
//   - default-src 'self'         fallback allow-same-origin for anything not listed
//   - script-src 'self'          no inline scripts; Vite emits only <script src="/assets/...">
//                                (verified: built index.html has no inline <script> bodies)
//   - style-src 'self' 'unsafe-inline'
//                                dynamic inline styles are pervasive (background-color from
//                                data, animation-delay, SVG stop-color); the F10 sanitizer
//                                guards the values. Removing inline styles would require a
//                                full refactor to data-attr + CSS — not worth the churn.
//   - img-src 'self' data:       the inline-SVG favicon uses a data: URL
//   - font-src 'self' data:      future-proofing; current build inlines no fonts
//   - connect-src 'self' ws: wss: WebSocket (same-origin by default; VITE_WS_URL may
//                                point cross-origin in asymmetric deployments)
//   - manifest-src 'self'        the PWA manifest lives at /manifest.webmanifest
//   - object-src 'none'          no Flash/Java/PDF plugins — classroom browsers don't need them
//   - base-uri 'self'            a injected <base> cannot redirect resource loads
//   - form-action 'self'         forms (the dashboard login) can only post back to self
//
// frame-ancestors is intentionally absent: it is not enforced by <meta>
// CSP (only by the HTTP header). Anti-clickjacking is handled by the
// Caddyfile's `X-Frame-Options: DENY`. Add a `Content-Security-Policy`
// response header in Caddy if/when frame-ancestors needs enforcing.
const cspPolicy = [
  "default-src 'self'",
  "script-src 'self'",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data:",
  "font-src 'self' data:",
  "connect-src 'self' ws: wss:",
  "manifest-src 'self'",
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'self'"
].join('; ')

function cspMeta() {
  return {
    name: 'vote-csp-meta',
    apply: 'build',
    transformIndexHtml(html) {
      const tag = `<meta http-equiv="Content-Security-Policy" content="${cspPolicy}" />`
      if (html.includes('Content-Security-Policy')) {
        return html // idempotent: a previous run already injected it
      }
      // Insert immediately after <meta charset> so it parses before any
      // resource the parser would otherwise fetch speculatively.
      return html.replace(
        /<meta charset="UTF-8"\s*\/?>/,
        (match) => `${match}\n    ${tag}`
      )
    }
  }
}

export default defineConfig({
  resolve: {
    alias: {
      '@shared': resolve(__dirname, 'shared'),
    },
  },
  server: {
    proxy: {
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      }
    }
  },
  plugins: [cspMeta()],
  build: {
    // Pin the output syntax to ES2019 so optional chaining, nullish
    // coalescing, and other modern syntax used by app code is transpiled
    // down. Vite's default ("modules") targets browsers with native ESM
    // (~Chrome 87+); ES2019 keeps us working back to ~Chrome 80 / Firefox
    // 74 / Safari 13.1, which still covers every classroom browser we
    // have seen in the wild. The service worker is shipped untranspiled
    // (see scripts/sw.template.js) and avoids ES2020-only primitives.
    target: 'es2019',
    rollupOptions: {
      input: {
        formateur: resolve(__dirname, 'formateur/index.html'),
        stagiaire: resolve(__dirname, 'stagiaire/index.html'),
      }
    }
  }
})
