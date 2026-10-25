import { resolve } from 'node:path'
import { defineConfig, mergeConfig } from 'vite'
import applicationConfig from '../../vite.config'

export default defineConfig(async (environment) => {
  const output = process.env.REN2HUB_UI_OUTPUT
  if (!output) throw new Error('An isolated UI output directory is required')
  const application = await (typeof applicationConfig === 'function'
    ? applicationConfig(environment)
    : applicationConfig)
  // Parallel evidence runs must not replace each other's optimized modules.
  return mergeConfig(application, { cacheDir: resolve(output, 'vite-cache') })
})
