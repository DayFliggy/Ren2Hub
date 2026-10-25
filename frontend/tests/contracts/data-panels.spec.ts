import { readFileSync } from 'node:fs'
import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { modelMarketMessages } from '../../src/i18n/modelMarket'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import { dataPanelsFixture } from './data-panels-fixture'
import { captureUI } from './ui-coverage'

const scenarios = [
  {
    key: 'desktop-light-zh',
    locale: 'zh-CN',
    theme: 'light',
    width: 1440,
    height: 900,
  },
  {
    key: 'mobile-dark-en',
    locale: 'en',
    theme: 'dark',
    width: 390,
    height: 844,
  },
] as const

async function captureTargets(
  page: Page,
  info: TestInfo,
  label: string,
  paths: string[]
) {
  await assertNoHorizontalOverflow(page)
  await captureUI(page, info, label)
  const capture = JSON.parse(
    readFileSync(info.outputPath(`${label}.coverage.json`), 'utf8')
  ) as { visible: string[] }
  for (const path of paths)
    expect(capture.visible, `${label}: actual rendered component`).toContain(
      `src/components/${path}.vue`
    )
}

for (const scenario of scenarios) {
  test(`dashboard statistics routing and order overview ${scenario.key}`, async ({
    page,
  }, info) => {
    test.setTimeout(120_000)
    await page.setViewportSize(scenario)
    await configureStablePage(page, scenario)
    const fixture = await dataPanelsFixture(page)
    await page.goto('/dashboard')
    await waitForStablePage(page)
    await page.locator('#dashboard-tab-panel-tab-stats').click()
    await expect(page.locator('[data-stats-kpi]')).toBeVisible()
    await expect(page.locator('[data-stats-model-scroll]')).toContainText(
      'gpt-native'
    )
    await captureTargets(page, info, 'dashboard-statistics', [
      'console/dashboard/stats/StatsKpiRow',
      'console/dashboard/stats/StatsDualTrend',
      'console/dashboard/stats/StatsModelTable',
      'console/dashboard/stats/StatsHourlyChart',
    ])
    await page.locator('#dashboard-tab-panel-tab-autoroute').click()
    const group = page.locator('[data-route-group]').first()
    await expect(group).toBeVisible()
    await group.locator('button[aria-expanded]').first().click()
    await expect(group.locator('[data-route-channel]:visible')).toHaveCount(3)
    await group.locator('[data-route-more-toggle]').click()
    await expect(group.locator('[data-route-channel]:visible')).toHaveCount(4)
    await expect(group.locator('[data-route-ratio]').first()).toContainText('1')
    await captureTargets(page, info, 'dashboard-routing-expanded', [
      'console/dashboard/autoroute/AutoRoutePanel',
      'console/dashboard/autoroute/VendorRouteGroup',
      'console/dashboard/autoroute/ChannelScoreRow',
      'console/dashboard/autoroute/RouteHealthTimeline',
    ])

    await page.goto('/orders')
    await waitForStablePage(page)
    // Exercise the real tab control before capturing the statistics branch.
    await page.locator('#orders-tab-panel-tab-list').click()
    await page.locator('#orders-tab-panel-tab-overview').click()
    await expect(page.getByText('fixture.alice', { exact: true })).toBeVisible()
    await expect(page.getByRole('meter')).toHaveCount(2)
    await captureTargets(page, info, 'order-overview', [
      'console/orders/OrderStatCard',
      'console/orders/OrderRevenueChart',
      'console/orders/PaymentMethodShare',
      'console/orders/TopSpenders',
    ])
    expect(fixture.reads).toContain('/api/user/topup/stats')
    expect(fixture.reads).toContain('/api/user/topup/ranking')
    expect(fixture.unexpected).toEqual([])
  })

  test(`model market views public tiered prices and ranking history ${scenario.key}`, async ({
    page,
  }, info) => {
    test.setTimeout(120_000)
    await page.setViewportSize(scenario)
    await configureStablePage(page, {
      ...scenario,
      status: {
        HeaderNavModules: JSON.stringify({ pricing: true, rankings: true }),
      },
    })
    const fixture = await dataPanelsFixture(page)
    const copy = modelMarketMessages[scenario.locale]
    await page.goto('/models')
    await waitForStablePage(page)
    await page
      .getByRole('radio', { name: copy.views.channels, exact: true })
      .click()
    await expect(page.locator('[data-market-shelves]')).toBeVisible()
    await captureTargets(page, info, 'model-channel-shelves', [
      'console/models/ModelChannelShelves',
    ])
    await page
      .locator('[data-market-shelves] [data-offer-id="gpt-native:2"]')
      .click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await expect(dialog.locator('[data-market-detail]')).toContainText(
      '/v1/chat/completions'
    )
    await captureTargets(page, info, 'model-detail', [
      'console/models/ModelMarketDetails',
    ])
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await page
      .getByRole('radio', { name: copy.views.comparison, exact: true })
      .click()
    await expect(
      page.locator(
        scenario.width < 640
          ? '[data-market-matrix-mobile]'
          : '[data-market-matrix]'
      )
    ).toBeVisible()
    await captureTargets(page, info, 'model-price-matrix', [
      'console/models/ModelPriceMatrix',
    ])

    // /models currently projects native fixed prices only. The existing public
    // detail route is the real tiered-expression entry for ReferencePrices.
    await page.goto('/pricing/gpt-dynamic-fixture')
    await waitForStablePage(page)
    const reference = page.locator('[data-reference-prices]')
    await expect(reference).toBeVisible()
    await expect(
      reference.locator('[data-price-dimension="input"]')
    ).toContainText('$2')
    await expect(
      reference.locator('[data-price-dimension="output"]')
    ).toContainText('$8')
    await captureTargets(page, info, 'public-tiered-reference-prices', [
      'console/models/ReferencePrices',
    ])

    await page.goto('/rankings')
    await waitForStablePage(page)
    await expect(page.locator('[role="img"]')).toHaveCount(2)
    await expect(
      page.locator('[role="img"]').first().locator('canvas')
    ).toHaveCount(1)
    await captureTargets(page, info, 'ranking-history', [
      'public/RankingHistoryChart',
    ])
    expect(fixture.reads).toContain('/api/pricing')
    expect(fixture.reads).toContain('/api/rankings')
    expect(fixture.unexpected).toEqual([])
  })
}
