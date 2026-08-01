// Vite configuration: Vue 3 + Tailwind v4, build output embedded by the Go
// binary from ../web/dist, dev proxy for /api to the Go server on :8080.
// publicDir points at ../web/static so vendored fonts are copied into the
// build (and served in dev) with zero external requests at runtime.
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  publicDir: '../web/static',
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: '../web/dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'jsdom',
  },
})
