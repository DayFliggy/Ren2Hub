import { readFile } from 'node:fs/promises'
import { expect, test, type Locator } from '@playwright/test'
import { nativeCoreFixture } from './native-core-fixture'

async function modalScrollGeometry(dialog: Locator, fraction: number) {
  return dialog.evaluate((root, scrollFraction) => {
    const body = root.querySelector<HTMLElement>('.subtle-scroll')!
    body.scrollTop = (body.scrollHeight - body.clientHeight) * scrollFraction
    const header = root.querySelector('header')!
    const footer = root.querySelector('footer')!
    const bodyBox = body.getBoundingClientRect()
    return {
      scrollTop: body.scrollTop,
      maximumScroll: body.scrollHeight - body.clientHeight,
      headerGap:
        bodyBox.top - header.lastElementChild!.getBoundingClientRect().bottom,
      footerGap:
        footer.firstElementChild!.getBoundingClientRect().top - bodyBox.bottom,
      headerTop: header.getBoundingClientRect().top,
      footerTop: footer.getBoundingClientRect().top,
    }
  }, fraction)
}

for (const theme of ['light', 'dark']) {
  for (const width of [1440, 390]) {
    test(`${theme} ${width} Key Codex and Pi configuration`, async ({
      page,
      context,
    }, testInfo) => {
      await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 })
      await page.emulateMedia({ reducedMotion: 'reduce' })
      await context.grantPermissions(['clipboard-read', 'clipboard-write'])
      const state = await nativeCoreFixture(page, theme, 'USD', {
        status: { server_address: 'http://localhost:3000' },
      })
      const secretRequests: string[] = []
      await page.route('**/api/pricing', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: [
              {
                model_name: 'gpt-native',
                description: 'Native model',
                quota_type: 0,
                model_ratio: 1,
                completion_ratio: 2,
                supported_endpoint_types: ['openai', 'openai-response'],
              },
            ],
            vendors: [],
            group_ratio: { default: 1 },
            usable_group: { default: 'Default' },
            supported_endpoint: {
              openai: { path: '/v1/chat/completions', method: 'POST' },
              'openai-response': { path: '/v1/responses', method: 'POST' },
            },
          },
        })
      )
      page.on('request', (request) => {
        if (
          /\/api\/token\/\d+\/(key|client-config)(?:\?|$)/.test(request.url())
        )
          secretRequests.push(request.url())
      })
      await page.route('**/api/token/7', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              id: 7,
              name: 'Native Key',
              key: 'sk-****1234',
              type: 'manual',
              status: 1,
              remain_quota: 500000,
              used_quota: 250000,
              unlimited_quota: false,
              group: 'default',
              auto_groups: null,
              cross_group_retry: false,
              model_limits_enabled: false,
              model_limits: '',
              allow_ips: null,
              created_time: 1,
              expired_time: -1,
            },
          },
        })
      )
      await page.goto('/keys')
      const open = page
        .getByRole('button', { name: 'Client configuration', exact: true })
        .filter({ visible: true })
      await expect(open).toHaveCount(1)
      for (const name of ['Copy API endpoint', 'Test API endpoint latency']) {
        const action = page.getByRole('button', { name, exact: true })
        await action.click({ trial: true })
        const bounds = await action.boundingBox()
        expect(bounds).not.toBeNull()
        expect(bounds!.x).toBeGreaterThanOrEqual(0)
        expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width)
        if (width === 390) {
          expect(bounds!.width).toBeGreaterThanOrEqual(44)
          expect(bounds!.height).toBeGreaterThanOrEqual(44)
        }
      }
      await page.screenshot({ path: testInfo.outputPath('keys-list.png') })
      await open.click()
      const dialog = page.getByRole('dialog', {
        name: 'Client configuration',
        exact: true,
      })
      await expect(
        dialog.getByRole('textbox', { name: 'Configuration preview' })
      ).toHaveValue(/wire_api = "responses"/)
      await expect(
        dialog.getByLabel('Client server address', { exact: true })
      ).toHaveValue('http://localhost:3000')
      await dialog
        .getByRole('button', { name: 'Use current site address' })
        .click()
      await expect(
        dialog.getByLabel('Client server address', { exact: true })
      ).toHaveValue(new URL('/', page.url()).href)
      await dialog
        .getByLabel('Client server address', { exact: true })
        .fill('https://gateway.example/proxy/')
      await dialog
        .getByRole('button', { name: 'Generate configuration', exact: true })
        .click()
      const preview = dialog.getByRole('textbox', {
        name: 'Configuration preview',
      })
      await expect(preview).toHaveValue(
        /base_url = "https:\/\/gateway.example\/proxy\/v1"/
      )
      const codex = await preview.inputValue()
      await dialog
        .getByRole('button', { name: 'Copy configuration', exact: true })
        .click()
      await expect
        .poll(() => page.evaluate(() => navigator.clipboard.readText()))
        .toBe(codex)
      await page.screenshot({ path: testInfo.outputPath('codex-preview.png') })
      const codexDownload = page.waitForEvent('download')
      await dialog
        .getByRole('button', { name: 'Download', exact: true })
        .click()
      const download = await codexDownload
      expect(download.suggestedFilename()).toBe('ren2hub.config.toml')
      expect(await readFile((await download.path())!, 'utf8')).toBe(codex)

      await dialog.getByLabel('Client', { exact: true }).selectOption('pi')
      await expect(preview).toHaveCount(0)
      await dialog
        .getByRole('button', { name: 'Generate configuration', exact: true })
        .click()
      await expect(dialog.getByRole('alert')).toContainText('positive integer')
      await dialog
        .getByLabel('Context window (tokens)', { exact: true })
        .fill('64000')
      await dialog
        .getByLabel('Maximum output (tokens)', { exact: true })
        .fill('8000')
      await dialog
        .getByLabel('Reasoning support', { exact: true })
        .selectOption('yes')
      await dialog
        .getByLabel('Input types', { exact: true })
        .selectOption('image')
      await page.screenshot({
        path: testInfo.outputPath('pi-capabilities.png'),
      })
      await dialog
        .getByRole('button', { name: 'Generate configuration', exact: true })
        .click()
      await expect(preview).toHaveValue(/"contextWindow": 64000/)
      const pi = await preview.inputValue()
      expect(JSON.parse(pi).providers.ren2hub).toMatchObject({
        apiKey: '${REN2HUB_API_KEY}',
        api: 'openai-completions',
        baseUrl: 'https://gateway.example/proxy/v1',
        models: [
          {
            id: 'gpt-native',
            contextWindow: 64000,
            maxTokens: 8000,
            reasoning: true,
            input: ['text', 'image'],
          },
        ],
      })
      await preview.scrollIntoViewIfNeeded()
      await page.screenshot({ path: testInfo.outputPath('pi-preview.png') })
      const fixedPositions = await modalScrollGeometry(dialog, 0)
      expect(fixedPositions.maximumScroll).toBeGreaterThan(0)
      for (const [position, fraction] of [
        ['top', 0],
        ['middle', 0.5],
        ['bottom', 1],
      ] as const) {
        const geometry = await modalScrollGeometry(dialog, fraction)
        expect(geometry.headerGap).toBeGreaterThanOrEqual(16)
        expect(geometry.footerGap).toBeGreaterThanOrEqual(16)
        expect(geometry.headerTop).toBe(fixedPositions.headerTop)
        expect(geometry.footerTop).toBe(fixedPositions.footerTop)
        if (fraction > 0) expect(geometry.scrollTop).toBeGreaterThan(0)
        await testInfo.attach(`pi-scroll-${position}-geometry`, {
          body: JSON.stringify(geometry),
          contentType: 'application/json',
        })
        await page.screenshot({
          path: testInfo.outputPath(`pi-scroll-${position}.png`),
        })
      }
      const piDownload = page.waitForEvent('download')
      await dialog
        .getByRole('button', { name: 'Download', exact: true })
        .click()
      const downloadedPi = await piDownload
      expect(downloadedPi.suggestedFilename()).toBe('ren2hub.models.json')
      expect(await readFile((await downloadedPi.path())!, 'utf8')).toBe(pi)
      await dialog.getByRole('button', { name: 'Close', exact: true }).click()
      await expect(dialog).toHaveCount(0)
      expect(secretRequests).toEqual([])
      expect(
        state.writes.filter((write) => write.path !== '/api/user/auth/refresh')
      ).toEqual([])
      expect(state.errors).toEqual([])
      expect(state.missingMessages).toEqual([])
    })
  }
}
