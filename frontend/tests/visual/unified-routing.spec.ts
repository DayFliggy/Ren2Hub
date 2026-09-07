import { expect, test } from '@playwright/test'
import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

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
      const row = page.getByRole('row').filter({ hasText: 'gpt-4.1' })
      await expect(row).toContainText('US$2.00 - US$3.00')
      await expect(row).toContainText('US$8.00 - US$12.00')
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)
      await page.screenshot({
        path: testInfo.outputPath('pricing.png'),
        fullPage: true,
      })

      const search = page.getByRole('searchbox', { name: '搜索模型或供应商' })
      await search.fill('no-matching-model')
      await expect(page.getByText('没有匹配的模型')).toBeVisible()
      await search.fill('gpt-4.1')
      await page.getByRole('link', { name: 'gpt-4.1', exact: true }).click()
      await expect(page.getByText('渠道倍率: 0.8 - 1.2')).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('price-detail.png'),
        fullPage: true,
      })

      await page.goto('/keys', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)
      await expect(
        page.getByText('Production key').filter({ visible: true })
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

test('pricing loading and error can recover to the channel catalog', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light', authenticated: true })
  let release: (() => void) | undefined
  let recovered = false
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/pricing', async (route) => {
    if (recovered) return route.fallback()
    await pending
    await route.fulfill({
      json: { success: false, message: 'Catalog unavailable' },
    })
  })
  await page.goto('/pricing', { waitUntil: 'domcontentloaded' })
  await expect(page.getByRole('status', { name: '正在加载' })).toBeVisible()
  recovered = true
  release?.()
  await expect(page.getByText('Catalog unavailable')).toBeVisible()
  await page.getByRole('alert').getByRole('button', { name: '重试' }).click()
  await expect(
    page.getByRole('link', { name: 'gpt-4.1', exact: true })
  ).toBeVisible()
})
