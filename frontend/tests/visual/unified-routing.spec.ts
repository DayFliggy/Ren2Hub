import { expect, test } from '@playwright/test'
import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage as configureNativePage,
  waitForStablePage,
} from './fixtures'

async function configureStablePage(
  ...args: Parameters<typeof configureNativePage>
) {
  await configureNativePage(...args)
  await args[0].route('**/api/pricing', (route) =>
    route.fulfill({
      json: {
        success: true,
        vendors: [],
        supported_endpoint: {
          openai: { path: '/v1/chat/completions', method: 'POST' },
        },
        data: [
          {
            model_name: 'gpt-4.1',
            quota_type: 0,
            model_ratio: 1.25,
            completion_ratio: 4,
            cache_ratio: 0.25,
            create_cache_ratio: 1.25,
            supported_endpoint_types: ['openai'],
          },
        ],
      },
    })
  )
}

for (const theme of ['light', 'dark'] as const) {
  for (const viewport of ['desktop', 'mobile'] as const) {
    test(`${theme} ${viewport} unified pricing and token creation`, async ({
      page,
    }, testInfo) => {
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.setViewportSize(
        viewport === 'desktop'
          ? { width: 1440, height: 900 }
          : { width: 390, height: 844 }
      )
      await configureStablePage(page, { theme, authenticated: true })
      await page.goto('/pricing', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      const card = page.getByRole('article').filter({
        has: page.getByRole('heading', { name: 'gpt-4.1', exact: true }),
      })
      const priceLabels = [
        '输入单价',
        '输出单价',
        '缓存读取单价',
        '缓存写入单价',
      ]
      const priceValues = [
        '$2.500 / 百万 Token',
        '$10.000 / 百万 Token',
        '$0.625 / 百万 Token',
        '$3.125 / 百万 Token',
      ]
      await expect(card.getByRole('term')).toHaveText(priceLabels)
      await expect(card.getByRole('definition')).toHaveText(priceValues)
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)
      await page.screenshot({
        path: testInfo.outputPath('pricing.png'),
        fullPage: true,
      })

      const search = page.getByRole('searchbox', {
        name: '搜索模型',
        exact: true,
      })
      await search.fill('no-matching-model')
      await expect(page.getByRole('article')).toHaveCount(0)
      await expect(
        page.getByText('暂无已发布价格', { exact: true })
      ).toBeVisible()
      await search.fill('gpt-4.1')
      await expect(page.getByRole('article')).toHaveCount(1)
      await page.getByRole('link', { name: 'gpt-4.1', exact: true }).click()
      await expect(page).toHaveURL(/\/pricing\/gpt-4\.1\?tokenUnit=M$/)
      await expect(
        page.getByRole('heading', { level: 1, name: 'gpt-4.1', exact: true })
      ).toBeVisible()
      // Public prices now expose model ratios, not retired scoped offer evidence.
      const detailPrices = page.locator('dl').filter({ hasText: '输入单价' })
      await expect(detailPrices.getByRole('term')).toHaveText(priceLabels)
      await expect(detailPrices.getByRole('definition')).toHaveText(priceValues)
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('price-detail.png'),
        fullPage: true,
      })

      await page.goto('/keys', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      await expect(
        page.getByText('Native Key').filter({ visible: true })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)
      await page.screenshot({
        path: testInfo.outputPath('keys.png'),
        fullPage: true,
      })
      const create = page.getByRole('button', { name: '创建令牌', exact: true })
      await create.click()
      const dialog = page.getByRole('dialog', { name: '创建令牌', exact: true })
      await expect(dialog).toBeVisible()
      await expect(
        dialog.getByRole('radio', { name: /自动令牌/ })
      ).toHaveAttribute('aria-checked', 'true')
      const manual = dialog.getByRole('radio', { name: /手动令牌/ })
      await manual.focus()
      await page.keyboard.press('Space')
      await expect(manual).toHaveAttribute('aria-checked', 'true')
      await dialog.locator('input[name="token-name"]').fill('Unified routing')
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('create-token.png'),
        fullPage: true,
      })

      const submitted: Record<string, unknown>[] = []
      await page.route('**/api/token/', async (route) => {
        if (route.request().method() !== 'POST') return route.fallback()
        submitted.push(
          route.request().postDataJSON() as Record<string, unknown>
        )
        await route.fulfill({ json: { success: true, message: '' } })
      })
      await dialog.getByRole('button', { name: '确认', exact: true }).click()
      await expect(dialog).toHaveCount(0)
      expect(submitted).toHaveLength(1)
      expect(submitted[0]).toMatchObject({
        name: 'Unified routing',
        type: 'manual',
      })
      expect(submitted[0]).not.toHaveProperty('group')
      expect(submitted[0]).not.toHaveProperty('auto_groups')
      expect(submitted[0]).not.toHaveProperty('cross_group_retry')
      await expect(create).toBeFocused()
      const failedImages = await page
        .locator('img')
        .evaluateAll((images) =>
          images
            .filter((image) => !image.complete || image.naturalWidth === 0)
            .map((image) => image.src)
        )
      expect(failedImages).toEqual([])
      expect(errors).toEqual([])
    })
  }
}

test('pricing loading and error can recover to the model catalog', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', authenticated: true })
  let release: (() => void) | undefined
  let recovered = false
  let requests = 0
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/pricing', async (route) => {
    requests++
    if (recovered) return route.fallback()
    await pending
    await route.fulfill({
      json: { success: false, message: 'Catalog unavailable' },
    })
  })
  await page.goto('/pricing', { waitUntil: 'domcontentloaded' })
  await expect(
    page.getByRole('status').filter({ hasText: '正在获取' })
  ).toBeVisible()
  recovered = true
  release?.()
  await expect(page.getByText('Catalog unavailable')).toBeVisible()
  const requestsBeforeRetry = requests
  await page.getByRole('alert').getByRole('button', { name: '重试' }).click()
  await expect.poll(() => requests).toBeGreaterThan(requestsBeforeRetry)
  await expect(
    page.getByRole('link', { name: 'gpt-4.1', exact: true })
  ).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
})
