import { defineConfig } from '@playwright/test'
import contractConfig from './playwright.config'

export default defineConfig({
  ...contractConfig,
  testMatch: ['frontend-audit.spec.ts', 'frontend-display.spec.ts'],
})
