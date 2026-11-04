import { expect, test, type Page, type TestInfo } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'

async function capture(page: Page, info: TestInfo, name: string) {
  if (process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS !== 'true') return
  const path = info.outputPath(`${name}.png`)
  await page.screenshot({ path, fullPage: true })
  await info.attach(name, { path, contentType: 'image/png' })
}

for (const theme of ['light', 'dark'] as const) {
  for (const width of [1440, 390]) {
    test(`key form keyboard and collapsed limits ${theme} ${width}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme, authenticated: true })
      const writes: Record<string, unknown>[] = []
      await page.route('**/api/token/**', async (route) => {
        if (route.request().method() !== 'POST') return route.fallback()
        writes.push(route.request().postDataJSON())
        await route.fulfill({ json: { success: true, data: null } })
      })
      await page.goto('/keys')
      await waitForStablePage(page)
      await page.getByRole('button', { name: '创建令牌', exact: true }).click()
      const dialog = page.getByRole('dialog')
      const manual = dialog.getByRole('radio', { name: /手动令牌/ })
      const auto = dialog.getByRole('radio', { name: /自动令牌/ })
      const expiry = dialog.getByRole('switch', {
        name: '过期时间',
        exact: true,
      })
      const unlimited = dialog.getByRole('switch', {
        name: '无限额度',
        exact: true,
      })
      const advanced = dialog.getByRole('button', { name: /高级设置/ })
      const expiration = dialog.locator('[name="token-expiration"]')
      const quota = dialog.locator('[name="token-quota"]')

      await expect(auto).toHaveAttribute('aria-checked', 'true')
      await auto.focus()
      await page.keyboard.press('ArrowLeft')
      await expect(manual).toBeFocused()
      await expect(manual).toHaveAttribute('aria-checked', 'true')
      await page.keyboard.press('ArrowRight')
      await expect(auto).toBeFocused()
      await expect(auto).toHaveAttribute('aria-checked', 'true')
      await page.keyboard.press('Tab')
      await expect(dialog.locator('[name="token-name"]')).toBeFocused()

      // Visually collapsed fields must also leave the keyboard sequence.
      await expiry.focus()
      await page.keyboard.press('Tab')
      await expect(unlimited).toBeFocused()
      await page.keyboard.press('Tab')
      await expect(advanced).toBeFocused()
      await capture(page, info, 'create-collapsed')

      await expiry.click()
      await page.keyboard.press('Tab')
      await expect(expiration).toBeFocused()
      await expiration.fill('2027-01-02T08:15:45')
      await unlimited.click()
      await page.keyboard.press('Tab')
      await expect(quota).toBeFocused()
      await quota.fill('20.125')
      await assertNoHorizontalOverflow(page)
      await capture(page, info, 'limits-expanded')

      // Hiding a limit preserves its draft but excludes it from the payload.
      await expiry.click()
      await unlimited.click()
      await expiry.focus()
      await page.keyboard.press('Tab')
      await expect(unlimited).toBeFocused()
      await page.keyboard.press('Tab')
      await expect(advanced).toBeFocused()
      await expiry.click()
      await unlimited.click()
      await expect(expiration).toHaveValue('2027-01-02T08:15:45')
      await expect(quota).toHaveValue('20.125')
      await expiry.click()
      await unlimited.click()

      await advanced.click()
      const permission = dialog.getByRole('switch', {
        name: '允许查询账户额度',
      })
      await expect(permission).toHaveAttribute('aria-checked', 'true')
      // The visible label must toggle its associated control exactly once.
      await dialog
        .locator('label')
        .filter({ hasText: '允许查询账户额度' })
        .click()
      await expect(permission).toHaveAttribute('aria-checked', 'false')
      await expect(advanced).toContainText('已配置 1 项')
      await capture(page, info, 'advanced-expanded')
      const ips = dialog.locator('[name="token-ip-limits"]')
      await ips.scrollIntoViewIfNeeded()
      await ips.focus()
      await expect(ips).toBeFocused()
      expect(
        await ips.evaluate((element) => {
          const box = element.getBoundingClientRect()
          return (
            document.elementFromPoint(
              box.x + box.width / 2,
              box.y + box.height / 2
            ) === element
          )
        })
      ).toBe(true)
      await capture(page, info, 'advanced-bottom')
      await advanced.click()
      await expect(permission).toBeHidden()
      await dialog.locator('[name="token-name"]').fill('keyboard-test')
      await dialog.getByRole('button', { name: '确认', exact: true }).click()
      await expect(dialog).toBeHidden()
      expect(writes).toEqual([
        expect.objectContaining({
          type: 'auto',
          allow_user_quota_query: false,
          expired_time: -1,
          unlimited_quota: true,
          remain_quota: 0,
        }),
      ])
      await assertNoHorizontalOverflow(page)
    })
  }

  for (const { width, hasTouch } of [
    ...[320, 768, 1023, 1024, 1440].map((width) => ({
      width,
      hasTouch: false,
    })),
    { width: 1440, hasTouch: true },
  ]) {
    test.describe(hasTouch ? 'touch pointer' : 'mouse pointer', () => {
      test.use({ hasTouch })
      test(`key list long name and multiple endpoints ${theme} ${width}`, async ({
        page,
      }, info) => {
        await page.setViewportSize({ width, height: 900 })
        await configureStablePage(page, {
          theme,
          authenticated: true,
          status: {
            api_info_enabled: true,
            api_info: [1, 2, 3].map((index) => ({
              url: `https://api-${index}.example.test/v1`,
              route: `线路 ${index}`,
              description: '',
            })),
          },
        })
        const longName = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWX'
        await page.route('**/api/token/?*', (route) =>
          route.fulfill({
            json: {
              success: true,
              data: {
                page: 1,
                page_size: 20,
                total: 1,
                items: [
                  {
                    id: 7,
                    name: longName,
                    key: 'sk-****1234',
                    type: 'manual',
                    status: 1,
                    remain_quota: 500000,
                    used_quota: 250000,
                    unlimited_quota: false,
                    model_limits_enabled: false,
                    model_limits: '',
                    allow_ips: null,
                    allow_user_quota_query: true,
                    created_time: 1785124800,
                    expired_time: -1,
                  },
                ],
              },
            },
          })
        )
        await page.goto('/keys')
        await waitForStablePage(page)
        await expect(
          page.getByText(longName, { exact: true }).filter({ visible: true })
        ).toBeVisible()
        await expect(
          page.locator('code').filter({ hasText: 'https://api-' })
        ).toHaveCount(3)
        await assertNoHorizontalOverflow(page)
        const row = page
          .locator('[data-key-mobile-row]')
          .filter({ visible: true })
        if (await row.count()) {
          const name = await row
            .getByText(longName, { exact: true })
            .boundingBox()
          const state = await row
            .locator('header')
            .getByRole('button', { name: '启停', exact: true })
            .boundingBox()
          expect(name!.width).toBeGreaterThan(80)
          expect(name!.x + name!.width).toBeLessThanOrEqual(state!.x)
        } else {
          const nameCell = page.locator('td[headers$="-name"]')
          const nameBox = await nameCell
            .getByText(longName, { exact: true })
            .boundingBox()
          const nameCellBox = await nameCell.boundingBox()
          expect(nameBox!.x + nameBox!.width).toBeLessThanOrEqual(
            nameCellBox!.x + nameCellBox!.width
          )
          const actions = page.locator('td[headers$="-actions"]')
          const controls = actions.locator('button, a')
          await expect(controls).toHaveCount(5)
          const bounds = await controls.evaluateAll((nodes) =>
            nodes.map((node) => {
              const { x, y, width, height } = node.getBoundingClientRect()
              return { x, y, width, height }
            })
          )
          const cell = await actions.boundingBox()
          for (let i = 0; i < bounds.length; i++) {
            expect(bounds[i]!.width).toBeGreaterThanOrEqual(
              hasTouch || width < 1024 ? 44 : 32
            )
            expect(bounds[i]!.y).toBeCloseTo(bounds[0]!.y, 0)
            expect(bounds[i]!.x).toBeGreaterThanOrEqual(cell!.x)
            expect(bounds[i]!.x + bounds[i]!.width).toBeLessThanOrEqual(
              cell!.x + cell!.width
            )
            if (i > 0)
              expect(bounds[i]!.x).toBeGreaterThanOrEqual(
                bounds[i - 1]!.x + bounds[i - 1]!.width
              )
          }
        }
        await capture(page, info, 'key-list')
      })
    })
  }
}
