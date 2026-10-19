import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

for (const locale of ['zh-CN', 'en'])
  for (const theme of ['light', 'dark'] as const)
    for (const width of [1440, 390]) {
      test(`October 16 restored panels ${locale} ${theme} ${width}`, async ({
        page,
      }, info) => {
        test.setTimeout(120_000)
        await page.setViewportSize({ width, height: 900 })
        const fixture = await configureStablePage(page, { theme, locale })
        await page.route('**/api/user/topup/stats?*', (route) =>
          route.fulfill({
            json: {
              success: true,
              data: {
                total: 11,
                currencies: [
                  {
                    currency: 'CNY',
                    count: 3,
                    paid_count: 2,
                    paid_amount: '73',
                    credited_quota: 5000000,
                    unknown_amount_count: 0,
                  },
                  {
                    currency: 'USD',
                    count: 8,
                    paid_count: 4,
                    paid_amount: '8',
                    credited_quota: 4000000,
                    unknown_amount_count: 0,
                  },
                ],
                daily: [
                  {
                    date: '2026-07-26',
                    currency: 'CNY',
                    count: 3,
                    paid_count: 2,
                    paid_amount: '73',
                    credited_quota: 5000000,
                    unknown_amount_count: 0,
                  },
                  {
                    date: '2026-07-26',
                    currency: 'USD',
                    count: 8,
                    paid_count: 4,
                    paid_amount: '8',
                    credited_quota: 4000000,
                    unknown_amount_count: 0,
                  },
                ],
                start_time: 1782518400,
                end_time: 1785124800,
              },
            },
          })
        )
        for (const [name, path] of [
          ['ops', '/ops'],
          ['routing', '/keys/7/routing'],
          ['dashboard', '/dashboard'],
          ['orders', '/orders'],
          ['wallet', '/wallet'],
          ['gmpay', '/system-settings/billing/gmpay'],
          ['invoice', '/invoice'],
          ['activity', '/activity'],
        ]) {
          await page.goto(path!, { waitUntil: 'domcontentloaded' })
          await waitForStablePage(page)
          if (name === 'ops') {
            await expect(page.locator('[data-ops-gauge]')).toHaveCount(3)
            await expect(
              page.locator('[data-ops-section="resources"]')
            ).toBeVisible()
            await expect(page.locator('[data-ops-page]')).not.toContainText(
              '"memory_stats"'
            )
          }
          if (name === 'routing') {
            await expect(page.locator('[data-channel-id="2"]')).toContainText(
              'Native channel'
            )
            await expect(
              page.getByRole('radio', {
                name: locale === 'en' ? 'Manual routing' : '手动路由',
                exact: true,
              })
            ).toBeChecked()
          }
          if (name === 'dashboard') {
            await page
              .getByRole('tab', {
                name: locale === 'en' ? 'Auto Route' : '自动路由',
                exact: true,
              })
              .click()
            const group = page.locator('[data-route-group]').first()
            await group.getByRole('button').first().click()
            await expect(
              group.locator('[data-route-channel]').filter({ visible: true })
            ).toHaveCount(3)
            await group.locator('[data-route-more-toggle]').click()
            await expect(
              group.locator('[data-route-channel]').filter({ visible: true })
            ).toHaveCount(4)
            await expect(group.locator('[data-route-health-cell]')).toHaveCount(
              6
            )
          }
          if (name === 'orders') {
            await expect(
              page.getByText('¥73.00', { exact: true }).first()
            ).toBeVisible()
            const currency = page.getByRole('combobox', {
              name: locale === 'en' ? 'Currency' : '币种',
              exact: true,
            })
            await currency.click()
            await page.getByRole('option', { name: 'USD', exact: true }).click()
            await expect(
              page.getByText('$8.00', { exact: true }).first()
            ).toBeVisible()
            await expect(
              page.getByText('¥73.00', { exact: true }).first()
            ).toHaveCount(0)
          }
          if (name === 'invoice') {
            const input = page.getByPlaceholder(
              locale === 'en'
                ? 'Company name or individual name'
                : '请输入发票抬头（公司名称或个人姓名）'
            )
            await input.fill('Restoration audit')
            await expect(
              page.getByRole('button', {
                name: locale === 'en' ? 'Submit request' : '提交申请',
                exact: true,
              })
            ).toBeDisabled()
            expect(
              fixture.writes.some((write) => write.path.includes('invoice'))
            ).toBe(false)
          }
          await waitForStablePage(page)
          await assertNoHorizontalOverflow(page)
          expect(fixture.unexpected).toEqual([])
          expect(fixture.errors).toEqual([])
          expect(fixture.missingMessages).toEqual([])
          await page.screenshot({
            path: info.outputPath(`${name}.png`),
            animations: 'disabled',
          })
          // The console scrolls inside its shell; fullPage captures only the
          // first viewport. Inspect each Ops block after scrolling into view.
          if (name === 'ops') {
            const sections = page.locator('[data-ops-page] > .ops-area')
            for (let i = 0; i < (await sections.count()); i++) {
              await sections.nth(i).scrollIntoViewIfNeeded()
              await page.screenshot({
                path: info.outputPath(`ops-section-${i + 1}.png`),
                animations: 'disabled',
              })
            }
          }
          const scrollZone = page.locator('[data-console-scroll-zone]')
          const steps = await scrollZone.evaluate((element) =>
            Math.ceil(element.scrollHeight / (element.clientHeight * 0.8))
          )
          expect(steps).toBeLessThan(25)
          for (let step = 1; step < steps; step++) {
            await scrollZone.evaluate((element, index) => {
              element.scrollTo({
                top: index * element.clientHeight * 0.8,
                behavior: 'instant',
              })
            }, step)
            await page.evaluate(
              () => new Promise((resolve) => requestAnimationFrame(resolve))
            )
            await page.screenshot({
              path: info.outputPath(`${name}-scroll-${step}.png`),
              animations: 'disabled',
            })
          }
        }
        await page.goto('/invoice')
        await expect(
          page.getByPlaceholder(
            locale === 'en'
              ? 'Company name or individual name'
              : '请输入发票抬头（公司名称或个人姓名）'
          )
        ).toHaveValue('')
      })
    }
