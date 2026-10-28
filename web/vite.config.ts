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
      // PayPal's redirects and webhook go to the server; /paypal/returned is the web app's page.
      '^/paypal/(return|cancel|webhook)(\\?|$)': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
    },
  },
  test: {
    // e2e/ holds the Playwright journeys, which run against a live server through scripts/e2e.sh.
    exclude: [...configDefaults.exclude, 'e2e/**'],
  },
})
