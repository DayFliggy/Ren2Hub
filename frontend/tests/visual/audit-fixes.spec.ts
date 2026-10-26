import { createHash } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { configureStablePage, assertNoHorizontalOverflow } from './fixtures'
import {
  mockPriceFetch,
  nativePriceSource,
} from '../contracts/native-core-fixture'
import { PRICING_KEYS } from '../../src/api/pricingKeys'

const channel = {
  id: 157,
  name: 'Audit OpenAI Fixture',
  type: 1,
  lab: { id: 'openai', name: 'OpenAI', source: 'vendor' },
  status: 3,
  other_info: JSON.stringify({
    status_reason: `供应商请求失败，已自动禁用。\n${'LongProviderErrorWithoutSpaces'.repeat(12)}`,
    status_time: 1785124800,
  }),
  priority: 0,
  weight: 10,
  in_flight: 0,
  max_concurrency: 20,
  channel_ratio: '1',
  used_quota: 0,
  balance: 0,
  response_time: 0,
  test_time: 0,
  base_url: 'https://mock.example.test',
  models: 'gpt-local',
  setting: '{}',
  settings: '{}',
}
for (const theme of ['light', 'dark'] as const)
  for (const width of [1440, 390]) {
    for (const operate of [false, true])
      test(`channel runtime permissions and reason ${theme} ${width} operate=${operate}`, async ({
        page,
      }, info) => {
        await page.setViewportSize({ width, height: 900 })
        await configureStablePage(page, {
          theme,
          authenticated: true,
          user: {
            role: 10,
            permissions: {
              admin_permissions: {
                channel: { read: true, write: false, operate },
              },
            },
          },
        })
        await page.route(/\/api\/channel\/(?:\?.*)?$/, (route) =>
          route.fulfill({
            json: {
              success: true,
              data: {
                items: [channel],
                total: 1,
                page: 1,
                page_size: 20,
                type_counts: { '1': 1 },
              },
            },
          })
        )
        await page.goto('/channels')
        expect(
          await page.locator('[aria-label="编辑渠道"]:visible').count()
        ).toBe(0)
        const reason = page
          .getByRole('button', { name: '查看自动禁用原因', exact: true })
          .filter({ visible: true })
        await expect(reason).toBeVisible()
        await expect(
          page.locator('[aria-label="预览模型更新"]:visible')
        ).toHaveCount(operate ? 1 : 0)
        if (width > 640) {
          await reason.hover()
          await expect(page.getByRole('tooltip')).toContainText(
            'LongProviderErrorWithoutSpaces'
          )
          await page.mouse.move(0, 0)
          await reason.focus()
        } else await reason.click()
        await expect(page.getByRole('tooltip')).toContainText('禁用时间')
        await assertNoHorizontalOverflow(page)
        const tooltip = await page.getByRole('tooltip').boundingBox()
        expect(tooltip!.x).toBeGreaterThanOrEqual(0)
        expect(tooltip!.x + tooltip!.width).toBeLessThanOrEqual(width)
        await page.screenshot({ path: info.outputPath('disabled-reason.png') })
        await page.keyboard.press('Escape')
        await page
          .getByRole('button', { name: '渠道运行状态', exact: true })
          .filter({ visible: true })
          .click()
        const dialog = page.getByRole('dialog', { name: channel.name })
        await expect(dialog).toBeVisible()
        await expect(dialog.getByText('Codex 供应商用量')).toHaveCount(0)
        await assertNoHorizontalOverflow(page)
        await page.screenshot({ path: info.outputPath('channel-runtime.png') })
      })

    test(`price field and expression differences ${theme} ${width}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme })
      const current = {
        ModelPrice: 0,
        'billing_setting.billing_mode': 'tiered_expr',
        'billing_setting.billing_expr':
          'tier("base", p * 0.123456789012345678 + c * 2)',
      }
      const candidate = {
        ModelRatio: 0,
        'billing_setting.billing_mode': 'tiered_expr',
        'billing_setting.billing_expr':
          'tier("base", p * 0.123456789012345679 + c * 3)',
      }
      let saves = 0
      await page.route('**/api/channel/models_enabled', (route) =>
        route.fulfill({ json: { success: true, data: ['precise-model'] } })
      )
      await page.route('**/api/option/model_pricing**', (route) => {
        if (route.request().method() === 'PATCH') saves++
        return route.fulfill({
          json: {
            success: true,
            data: {
              entries: [
                {
                  model_name: 'precise-model',
                  version: '1',
                  configured: current,
                  effective: current,
                },
              ],
              options: Object.fromEntries(
                PRICING_KEYS.map((key) => [key, '{}'])
              ),
              empty_version: 'empty',
            },
          },
        })
      })
      await mockPriceFetch(page, [
        nativePriceSource({
          id: 'sub2api',
          name: 'Sub2API',
          status: 'success',
          errors: [],
          models: [
            {
              model_name: 'precise-model',
              provider: 'fixture',
              pricing: candidate,
            },
          ],
        }),
      ])
      await page.goto('/models/pricing')
      await page.getByRole('button', { name: '自动获取', exact: true }).click()
      const dialog = page.getByRole('dialog')
      await dialog.locator('input[value="sub2api"]').check()
      await expect(dialog.getByText('未设置', { exact: true })).toHaveCount(2)
      await expect(
        dialog
          .locator('[data-diff="added"]')
          .filter({ hasText: '0.123456789012345679' })
      ).toBeVisible()
      await expect(
        dialog
          .locator('[data-diff="removed"]')
          .filter({ hasText: '0.123456789012345678' })
      ).toBeVisible()
      expect(saves).toBe(0)
      await assertNoHorizontalOverflow(page)
      await page.screenshot({ path: info.outputPath('price-differences.png') })
      if (width < 640) {
        await dialog
          .getByRole('button', { name: '填入定价草稿' })
          .scrollIntoViewIfNeeded()
        await dialog.locator('.price-fetch-table').evaluate((element) => {
          element.scrollLeft = element.scrollWidth
        })
        await page.screenshot({
          path: info.outputPath('price-candidate-mobile.png'),
        })
      }
      await page.keyboard.press('Escape')
      expect(saves).toBe(0)
    })

    test(`market assets revalidate without credentials ${theme} ${width}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme, authenticated: true })
      await page.addInitScript(() => {
        const browser = window as typeof window & {
          marketRequests: Array<{
            url: string
            cache?: RequestCache
            credentials?: RequestCredentials
          }>
        }
        browser.marketRequests = []
        const original = window.fetch.bind(window)
        window.fetch = (input, options) => {
          if (String(input).startsWith('https://plugins.example/'))
            browser.marketRequests.push({
              url: String(input),
              cache: options?.cache,
              credentials: options?.credentials,
            })
          return original(input, options)
        }
      })
      const meta = {
        key: 'audit',
        name: 'Audit Market Plugin',
        version: '1.0.0',
        apiVersion: 1,
        models: ['audit'],
        icon: 'text:AM',
      }
      const source = `export const meta = ${JSON.stringify(meta)};`
      const hash = createHash('sha256').update(source).digest('hex')
      let installs = 0
      await page.route('**/api/plugin/task**', (route) => {
        const path = new URL(route.request().url()).pathname
        if (route.request().method() === 'POST') installs++
        const data = path.endsWith('/marketplace/sources')
          ? [
              {
                name: 'Audit Market',
                index_url: 'https://plugins.example/index.json',
              },
            ]
          : []
        return route.fulfill({ json: { success: true, data } })
      })
      await page.route('https://plugins.example/**', (route) => {
        const path = new URL(route.request().url()).pathname
        expect(route.request().headers()['cookie']).toBeUndefined()
        expect(route.request().headers()['authorization']).toBeUndefined()
        if (path.endsWith('index.json'))
          return route.fulfill({
            json: {
              indexVersion: 1,
              plugins: [
                {
                  ...meta,
                  latest: '1.0.0',
                  iconFile: { path: 'icon.svg' },
                  versions: [
                    { version: '1.0.0', path: 'plugin.js', sha256: hash },
                  ],
                },
              ],
            },
          })
        if (path.endsWith('icon.svg')) return route.fulfill({ status: 404 })
        return route.fulfill({ contentType: 'text/javascript', body: source })
      })
      await page.goto('/task-plugins')
      await page.getByRole('tab', { name: '插件市场', exact: true }).click()
      await expect(
        page.getByRole('heading', { name: meta.name, exact: true })
      ).toBeVisible()
      await expect(page.getByText('AM', { exact: true })).toBeVisible()
      await page.screenshot({
        path: info.outputPath('market-icon-fallback.png'),
      })
      await page.getByRole('button', { name: '安装', exact: true }).click()
      await expect(
        page.getByText('文件校验通过', { exact: true })
      ).toBeVisible()
      const requests = await page.evaluate(
        () =>
          (
            window as typeof window & {
              marketRequests: Array<{
                url: string
                cache?: RequestCache
                credentials?: RequestCredentials
              }>
            }
          ).marketRequests
      )
      expect(requests.map((request) => new URL(request.url).pathname)).toEqual(
        expect.arrayContaining(['/index.json', '/icon.svg', '/plugin.js'])
      )
      expect(
        requests.every(
          (request) =>
            request.cache === 'no-cache' && request.credentials === 'omit'
        )
      ).toBe(true)
      expect(installs).toBe(0)
      await assertNoHorizontalOverflow(page)
    })
  }
