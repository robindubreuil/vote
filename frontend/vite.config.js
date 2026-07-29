import { resolve } from 'path'
import { defineConfig } from 'vite'

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
