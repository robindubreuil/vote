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
    rollupOptions: {
      input: {
        formateur: resolve(__dirname, 'formateur/index.html'),
        stagiaire: resolve(__dirname, 'stagiaire/index.html'),
      }
    }
  }
})
