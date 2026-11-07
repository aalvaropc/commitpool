/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { configDefaults } from 'vitest/config'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '^/paypal/(return|cancel|webhook)(\\?|$)': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
      '/mcp': 'http://localhost:8080',
    },
  },
  test: {
    exclude: [...configDefaults.exclude, 'e2e/**'],
  },
})
