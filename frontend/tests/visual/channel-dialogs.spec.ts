import { expect, test, type Page } from '@playwright/test'

import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

const models = Array.from(
  { length: 12 },
  (_, index) => `model-${String(index + 1).padStart(2, '0')}`
)

const channels = [
  {
    id: 101,
    name: 'Anthropic Primary',
    type: 14,
    group: 'default',
    status: 1,
    priority: 10,
    weight: 100,

    used_quota: 245_000,
    balance: 92.5,
    response_time: 420,
    test_time: 1_786_742_400,
    base_url: 'https://api.anthropic.com',
    models: models.join(','),
    model_mapping: '',
  },
  {
    id: 102,
    name: 'Anthropic Reserve',
    type: 14,
    group: 'default',
    status: 1,
    priority: 5,
    weight: 50,

    used_quota: 120_000,
    balance: 48.25,
    response_time: 630,
    test_time: 1_786_742_400,
    base_url: 'https://api.anthropic.com',
    models: models.slice(0, 8).join(','),
    model_mapping: '',
  },
]

async function configureChannels(page: Page): Promise<() => void> {
  await page.route(/\/api\/channel\/(?:\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        success: true,
        message: '',
        data: {
          items: channels,
          total: channels.length,
          page: 1,
          page_size: 20,
          type_counts: { '14': channels.length },
        },
      }),
    })
  )

  let testGate = Promise.resolve()
  let releaseGate = () => {}
  const holdTests = () => {
    testGate = new Promise<void>((resolve) => {
      releaseGate = resolve
    })
  }

  await page.route('**/api/channel/test/*', async (route) => {
    await testGate
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        success: true,
        message: '',
        time: 0.123,
      }),
    })
  })

  holdTests()
  return () => {
    releaseGate()
    holdTests()
  }
}

async function expectInsideViewport(
  page: Page,
  selector: string
): Promise<void> {
  const rect = await page.locator(selector).evaluate((element) => {
    const box = element.getBoundingClientRect()
    return {
      top: box.top,
      right: box.right,
      bottom: box.bottom,
      left: box.left,
      viewportWidth: window.innerWidth,
      viewportHeight: window.innerHeight,
    }
  })
  expect(rect.left).toBeGreaterThanOrEqual(0)
  expect(rect.top).toBeGreaterThanOrEqual(0)
  expect(rect.right).toBeLessThanOrEqual(rect.viewportWidth)
  expect(rect.bottom).toBeLessThanOrEqual(rect.viewportHeight)
}

for (const theme of [
  'light',
  'dark',
] as const satisfies readonly VisualTheme[]) {
  for (const viewport of ['desktop', 'mobile'] as const) {
    test(`${theme} ${viewport} channel test dialogs remain contained`, async ({
      page,
    }) => {
      await page.setViewportSize(
        viewport === 'desktop'
          ? { width: 1440, height: 900 }
          : { width: 390, height: 844 }
      )
      await configureStablePage(page, { theme, authenticated: true })
      const releaseTests = await configureChannels(page)
      await page.goto('/channels', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)

      const navigation =
        viewport === 'desktop'
          ? page.locator('[data-handdrawn="navigation"]')
          : page.getByRole('navigation', { name: '控制台' })
      await expect(navigation).toBeVisible()
      await expect(navigation).toHaveCSS('user-select', 'none')
      await expect(
        navigation.locator('[data-console-nav-item="subscription"]')
      ).toHaveCount(0)
      await expect(
        navigation.locator('[data-console-nav-item="plan-management"]')
      ).toHaveCount(0)

      await page.locator('[aria-label="测试响应"]:visible').first().click()
      const channelDialog = page.getByRole('dialog', {
        name: '测试渠道连接：Anthropic Primary',
      })
      await expect(channelDialog).toBeVisible()
      await expect(channelDialog.locator('tbody tr')).toHaveCount(5)
      await expect(channelDialog).not.toContainText('共 12 项')
      await expectInsideViewport(page, '[role="dialog"][aria-modal="true"]')

      const footer = channelDialog.locator(
        '[data-pagination-variant="modal-footer"]'
      )
      const footerOrder = await footer.evaluate((element) => {
        const pageSize = element.querySelector('[data-pagination-page-size]')
        const controls = element.querySelector('[data-pagination-controls]')
        const actions = element.querySelector('[data-pagination-actions]')
        if (!pageSize || !controls || !actions) return []
        return [pageSize, controls, actions].map((item) =>
          Array.from(element.querySelectorAll('*')).indexOf(item)
        )
      })
      expect(footerOrder[0]).toBeLessThan(footerOrder[1]!)
      expect(footerOrder[1]).toBeLessThan(footerOrder[2]!)

      await footer.getByRole('combobox').click()
      await expect(page.getByRole('option')).toHaveText([
        '显示 5',
        '显示 10',
        '显示 30',
        '显示 50',
      ])
      await page.getByRole('option', { name: '显示 5', exact: true }).click()
      await footer.getByRole('button', { name: '下一页' }).click()
      await expect(channelDialog.locator('tbody tr')).toHaveCount(5)
      await expect(channelDialog.locator('tbody')).toContainText('model-06')
      await expect(channelDialog.locator('tbody')).not.toContainText('model-01')

      await channelDialog
        .getByRole('button', { name: '测试模型 model-06' })
        .click()
      await expect(
        channelDialog.locator('[data-channel-model-row-spinner]')
      ).toBeVisible()
      await expectInsideViewport(page, '[data-channel-model-row-spinner]')
      releaseTests()
      await expect(
        channelDialog.locator('[data-channel-model-row-spinner]')
      ).toHaveCount(0)

      await assertNoHorizontalOverflow(page)
      await footer.getByRole('button', { name: '关闭' }).click()
      await expect(channelDialog).toHaveCount(0)
      await assertInteractiveCentersVisible(page)
    })
  }
}
