import { expect, test } from '@playwright/test'
import { createModelMarketFixture } from '../fixtures/modelMarket'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  captureEvidence,
} from './fixtures'

// The layout assertions depend on width, not theme; themes alternate.
for (const [width, theme] of [
  [320, 'light'],
  [390, 'dark'],
  [768, 'light'],
  [1024, 'dark'],
  [1440, 'light'],
] as const) {
  test(`model market A/B/C and details ${theme} ${width}`, async ({
    page,
  }, testInfo) => {
    const errors: string[] = []
    const requests: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    page.on('request', (request) =>
      requests.push(new URL(request.url()).pathname)
    )
    await page.setViewportSize({ width, height: 900 })
    await configureStablePage(page, { theme })
    await page.goto('/models')
    await waitForStablePage(page)

    const card = page.locator('[data-model-card][data-model-name="gpt-4.1"]')
    await expect(card).toBeVisible()
    await expect(card.locator('[data-market-baseline]')).toContainText('$2.500')
    await expect(card.locator('[data-channel-quote]')).toContainText('$0.500')
    await expect(
      card.locator('[data-market-baseline] [data-market-telemetry]')
    ).toHaveCount(0)
    await expect(card).not.toContainText(/优惠|最优/)
    // Compact cards do not invent metrics when the native sample is empty.
    await expect(card.locator('[data-market-telemetry]')).toHaveCount(0)
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath('models.png'),
      fullPage: true,
    })

    await page.getByRole('radio', { name: '渠道货架', exact: true }).click()
    await expect(page.locator('.market-model-card')).toHaveCount(0)
    await expect(page.locator('[data-market-shelves]')).toBeVisible()
    await expect(
      page.getByRole('heading', { name: /^雾桥中转/ }).first()
    ).toBeVisible()
    await expect(page.locator('[data-market-telemetry]')).toHaveCount(0)
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath('channels.png'),
      fullPage: true,
    })

    await page.getByRole('radio', { name: '比价矩阵', exact: true }).click()
    const comparison = page.locator('.market-comparison')
    await expect(comparison.locator('[data-market-telemetry]')).toHaveCount(0)
    await expect(comparison).not.toContainText(
      /TPS|耗时|延迟|可用率|最优|省 \d+%/
    )
    const matrix = page.locator('[data-market-matrix]')
    if (await matrix.isVisible()) {
      await expect(matrix.locator('thead .benchmark-column')).toContainText(
        '平台基准价'
      )
      await expect(
        matrix.locator('tbody tr').first().locator('td').last()
      ).toHaveClass(/benchmark-column/)
      await matrix.locator('th[data-channel-id="11"]').hover()
      await expect(
        matrix.locator('[data-channel-id="11"].col-hover')
      ).toHaveCount(createModelMarketFixture().models.length + 1)
      await expect(
        matrix.locator('.benchmark-column.col-hover,.model-column.col-hover')
      ).toHaveCount(0)
    } else {
      const mobile = page.locator('[data-market-matrix-mobile]')
      await expect(mobile).toBeVisible()
      const group = mobile.locator('[data-model-name="gpt-4.1"]')
      await expect(group.locator('[data-market-baseline]')).toContainText(
        '$2.500'
      )
      await expect(
        group.locator('[data-price-dimension="output"]').first()
      ).toContainText('$10.000')
    }
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath('comparison.png'),
      fullPage: true,
    })

    await page.getByRole('radio', { name: '模型聚合', exact: true }).click()
    const trigger = card.getByRole('button', {
      name: '查看 gpt-4.1 的模型详情与渠道报价',
      exact: true,
    })
    await trigger.click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await expect(
      dialog.locator('[data-market-baseline] [data-market-telemetry]')
    ).toHaveCount(0)
    await expect(
      dialog.locator('[data-market-telemetry]').first()
    ).toContainText('统计')
    await expect(
      dialog.locator('[data-price-dimension="input"]').last()
    ).toContainText('$0.500')
    await expect(
      dialog.locator('[data-price-dimension="output"]').last()
    ).toContainText('$2.000')
    await expect(dialog).toContainText('/v1/chat/completions')
    await captureEvidence(page, { path: testInfo.outputPath('details.png') })
    for (let tab = 0; tab < 14; tab++) {
      await page.keyboard.press('Tab')
      expect(
        await dialog.evaluate((element) =>
          element.contains(document.activeElement)
        )
      ).toBe(true)
    }
    await assertNoHorizontalOverflow(page)
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await expect(trigger).toBeFocused()
    expect(
      requests.filter((path) => path === '/api/user/model-catalog')
    ).toHaveLength(1)
    expect(
      requests.filter((path) => path === '/api/perf-metrics/summary')
    ).toHaveLength(1)
    expect(errors).toEqual([])
  })
}

test('market filters persist across views and translated long names remain usable', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'dark' })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
  const fixture = createModelMarketFixture()
  const longId = `provider/${'long-model-name-without-break'.repeat(6)}`
  fixture.models[0]!.model_name = longId
  fixture.offers.forEach((offer) => {
    if (offer.model_id === 'gpt-4.1') offer.model_id = longId
  })
  await page.route('**/api/user/model-catalog', (route) =>
    route.fulfill({ json: { success: true, data: fixture } })
  )
  await page.goto('/models')
  await waitForStablePage(page)
  await page.getByRole('combobox', { name: /^Channel filter / }).click()
  await page.getByRole('option', { name: '原生专线', exact: true }).click()
  const search = page.getByPlaceholder('Search models, providers or channels…')
  await search.fill('provider/')
  for (const name of ['Channel shelves', 'Price comparison', 'Models']) {
    await page.getByRole('radio', { name, exact: true }).click()
    await expect(search).toHaveValue('provider/')
    await expect(
      page.getByRole('combobox', { name: /^Channel filter / })
    ).toContainText('原生专线')
    await assertNoHorizontalOverflow(page)
  }
  const trigger = page.getByRole('button', {
    name: `View details and channel prices for ${longId}`,
    exact: true,
  })
  await trigger.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText(longId)
  await expect(dialog).toContainText('Platform base price')
  await expect(dialog).toContainText('No per-channel statistics')
  await assertNoHorizontalOverflow(page)
  await dialog
    .getByRole('button', { name: 'Close model details', exact: true })
    .click()
  await expect(trigger).toBeFocused()
  await search.fill('no such model')
  await expect(
    page.getByText('No matching models', { exact: true })
  ).toBeVisible()
  await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
  await expect(page.locator('[data-model-card]')).toHaveCount(7)
})

test('comparison keeps model and platform columns fixed while scrolling and supports focus highlights', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await page.setViewportSize({ width: 1440, height: 900 })
  const fixture = createModelMarketFixture()
  for (const id of [14, 15, 16, 17]) {
    fixture.channels.push({
      ...fixture.channels[0]!,
      id,
      name: `Additional ${id}`,
    })
    fixture.offers.push(
      ...fixture.offers
        .filter((offer) => offer.channel_id === 11)
        .map((offer) => ({ ...offer, channel_id: id }))
    )
  }
  await page.route('**/api/user/model-catalog', (route) =>
    route.fulfill({ json: { success: true, data: fixture } })
  )
  await page.goto('/models')
  await waitForStablePage(page)
  await page.getByRole('radio', { name: '比价矩阵', exact: true }).click()
  const matrix = page.locator('[data-market-matrix]')
  const model = matrix.locator('tbody .model-column').first()
  const benchmark = matrix.locator('tbody .benchmark-column').first()
  const modelBefore = await model.boundingBox()
  const baseBefore = await benchmark.boundingBox()
  await matrix.evaluate((element) => {
    element.scrollLeft = 400
  })
  await expect
    .poll(() => matrix.evaluate((element) => element.scrollLeft))
    .toBeGreaterThan(0)
  expect((await model.boundingBox())!.x).toBeCloseTo(modelBefore!.x, 0)
  expect((await benchmark.boundingBox())!.x).toBeCloseTo(baseBefore!.x, 0)
  await expect(model).toHaveCSS('position', 'sticky')
  await expect(benchmark).toHaveCSS('position', 'sticky')
  const offer = matrix.locator('td[data-channel-id="14"] button').first()
  await offer.focus()
  await expect(matrix.locator('[data-channel-id="14"].col-hover')).toHaveCount(
    fixture.models.length + 1
  )
  await expect(
    matrix.locator('.benchmark-column.col-hover,.model-column.col-hover')
  ).toHaveCount(0)
  await page.getByRole('radio', { name: '比价矩阵', exact: true }).focus()
  await expect(matrix.locator('.col-hover')).toHaveCount(0)
  await assertNoHorizontalOverflow(page)
})

test('failed catalog loads stay visible as errors and recover through retry', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  let calls = 0
  await page.route('**/api/user/model-catalog', (route) => {
    calls++
    return calls === 1
      ? route.fulfill({
          status: 503,
          json: { success: false, message: 'Catalog temporarily unavailable' },
        })
      : route.fulfill({
          json: { success: true, data: createModelMarketFixture() },
        })
  })
  await page.goto('/models')
  await expect(
    page.getByText('模型广场加载失败', { exact: true })
  ).toBeVisible()
  await expect(page.locator('[data-model-card]')).toHaveCount(0)
  await page.getByRole('button', { name: '重新加载', exact: true }).click()
  await expect(page.locator('[data-model-card]')).toHaveCount(7)
  expect(calls).toBe(2)
})

// English copy keeps every width; each width uses the theme A/B/C does not.
for (const [width, theme] of [
  [320, 'dark'],
  [390, 'light'],
  [768, 'dark'],
  [1024, 'light'],
  [1440, 'dark'],
] as const) {
  test(`English market layout ${theme} ${width}`, async ({
    page,
  }, testInfo) => {
    await configureStablePage(page, { theme })
    await page.setViewportSize({ width, height: 900 })
    await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
    await page.goto('/models')
    await waitForStablePage(page)
    for (const name of ['Channel shelves', 'Price comparison', 'Models']) {
      await page.getByRole('radio', { name, exact: true }).click()
      await assertNoHorizontalOverflow(page)
      await captureEvidence(page, {
        path: testInfo.outputPath(`${name}.png`),
        fullPage: true,
      })
    }
    await page
      .getByRole('button', {
        name: 'View details and channel prices for gpt-4.1',
        exact: true,
      })
      .click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toContainText('Platform base price')
    await expect(dialog).toContainText('No per-channel statistics')
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: testInfo.outputPath('english-details.png'),
    })
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
  })
}

for (const [theme, width] of [
  ['light', 1440],
  ['dark', 1440],
  ['dark', 390],
] as const) {
  test(`single-line channel overflow rail and master-detail workbench ${theme} ${width}`, async ({
    page,
  }, testInfo) => {
    await configureStablePage(page, { theme })
    await page.setViewportSize({ width, height: 900 })
    const fixture = createModelMarketFixture()
    fixture.models[0]!.tags = '推理,视觉,代码,长上下文'
    for (const [id, name, ratio] of [
      [14, '低价聚合', '0.4'],
      [15, '极速直连', '0.8'],
      [16, '海外备用', '1.2'],
    ] as const) {
      fixture.channels.push({
        ...fixture.channels[0]!,
        id,
        name,
        channel_ratio: ratio,
      })
      fixture.offers.push({
        ...fixture.offers.find(
          (offer) => offer.model_id === 'gpt-4.1' && offer.channel_id === 11
        )!,
        channel_id: id,
        channel_ratio: ratio,
        pricing_values: {
          input: String(Number((2.5 * Number(ratio)).toFixed(4))),
          output: String(Number((10 * Number(ratio)).toFixed(4))),
        },
      })
    }
    const baseTs = 1_785_038_400
    const hourlySeries = Array.from({ length: 12 }, (_, index) => ({
      ts: baseTs + index * 3600,
      avg_ttft_ms: 180 + index * 12,
      avg_latency_ms: 720 + index * 25,
      success_rate: index === 7 ? 96.5 : 99.6,
      avg_tps: 45.2 - index * 0.4,
    }))
    await page.route('**/api/user/model-catalog', (route) =>
      route.fulfill({ json: { success: true, data: fixture } })
    )
    await page.route('**/api/perf-metrics/summary**', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            summary: { avg_latency_ms: 820, success_rate: 99.3, avg_tps: 43.1 },
            window_start: baseTs,
            window_end: baseTs + 86_400,
            models: [
              {
                model_name: 'gpt-4.1',
                avg_latency_ms: 820,
                success_rate: 99.3,
                avg_tps: 43.1,
                recent_success_series: hourlySeries.map(
                  ({ ts, success_rate }) => ({ ts, success_rate })
                ),
              },
            ],
          },
        },
      })
    )
    await page.route('**/api/perf-metrics?*', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            model_name: 'gpt-4.1',
            series_schema: 'v1',
            summary: { avg_latency_ms: 820, success_rate: 99.3, avg_tps: 43.1 },
            series: hourlySeries,
            window_start: baseTs,
            window_end: baseTs + 86_400,
          },
        },
      })
    )

    await page.goto('/models')
    await waitForStablePage(page)
    const card = page.locator('[data-model-card][data-model-name="gpt-4.1"]')
    const rail = card.locator('[data-channel-rail]')
    await expect(rail).toBeVisible()
    // All visible chips and overflow trigger stay on one horizontal line.
    const chips = rail.locator('[data-channel-chip]')
    const chipCount = await chips.count()
    expect(chipCount).toBeGreaterThanOrEqual(1)
    expect(chipCount).toBeLessThanOrEqual(3)
    const firstBox = (await chips.first().boundingBox())!
    for (let i = 1; i < chipCount; i++) {
      const box = (await chips.nth(i).boundingBox())!
      expect(Math.abs(box.y - firstBox.y)).toBeLessThanOrEqual(2)
    }
    const overflow = rail.locator('[data-channel-overflow]')
    await expect(overflow).toBeVisible()
    const overflowBox = (await overflow.boundingBox())!
    expect(Math.abs(overflowBox.y - firstBox.y)).toBeLessThanOrEqual(2)

    await overflow.click()
    const menu = card.locator('[data-channel-overflow-menu]')
    await expect(menu).toBeVisible()
    await captureEvidence(page, {
      path: testInfo.outputPath(`mode-a-overflow-${theme}-${width}.png`),
    })
    await menu.locator('[data-channel-overflow-item="16"]').click()
    await expect(menu).toHaveCount(0)
    await expect(card.locator('[data-channel-quote]')).toContainText('海外备用')
    await expect(card.locator('[data-channel-quote]')).toContainText('$3.000')
    await assertNoHorizontalOverflow(page)

    await card
      .getByRole('button', {
        name: '查看 gpt-4.1 的模型详情与渠道报价',
        exact: true,
      })
      .click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await expect(
      dialog.locator('[data-model-statistics-series] button')
    ).toHaveCount(12)
    await captureEvidence(page, {
      path: testInfo.outputPath(`detail-quote-${theme}-${width}.png`),
    })
    await dialog.getByRole('tab', { name: '接入', exact: true }).click()
    await expect(dialog).toContainText('https://api.example.test')
    await expect(dialog).toContainText('/v1/chat/completions')
    await expect(dialog).toContainText('curl ')
    await captureEvidence(page, {
      path: testInfo.outputPath(`detail-integrate-${theme}-${width}.png`),
    })
    await assertNoHorizontalOverflow(page)
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
  })
}
