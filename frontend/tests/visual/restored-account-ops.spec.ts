import { expect, test } from '@playwright/test'
import { nativeCoreFixture } from './native-core-fixture'
import { minTextContrast } from './fixtures'
for (const theme of ['light', 'dark'] as const)
  for (const width of [1440, 390]) {
    test(`restored account and operations ${theme} ${width}`, async ({
      page,
    }, testInfo) => {
      test.setTimeout(90_000)
      await page.setViewportSize({ width, height: 1000 })
      await page.emulateMedia({ reducedMotion: 'reduce' })
      await page
        .context()
        .grantPermissions(['clipboard-read', 'clipboard-write'])
      const fixture = await nativeCoreFixture(page, theme)
      const routes = [
        ['/dashboard', 'System status'],
        ['/profile', 'Native Root'],
        ['/activity', 'Daily check-in'],
        ['/invite', 'Monthly referral trend'],
        ['/ops', 'Operations overview'],
      ] as const
      for (const [path, title] of routes) {
        await page.goto(path)
        await expect(
          page.getByText(title, { exact: true }).first()
        ).toBeVisible()
        if (path === '/dashboard') {
          await expect(page.locator('[data-kpi-cell]')).toHaveCount(5)
          await expect(
            page.getByText('Live rates and limits are unavailable').first()
          ).toBeVisible()
          await page
            .getByRole('tab', { name: 'Automatic routing', exact: true })
            .click()
          const link = page.getByRole('link', {
            name: 'View Token routing',
            exact: true,
          })
          await expect(link).toHaveAttribute('href', '/keys')
          expect(
            fixture.writes.filter((write) => write.path.includes('/routing'))
          ).toEqual([])
          await page.screenshot({
            path: testInfo.outputPath(
              `dashboard-routing-${theme}-${width}.png`
            ),
            fullPage: true,
          })
          await page.getByRole('tab', { name: 'Overview', exact: true }).click()
          if (width < 640) {
            const table = page.locator('[data-model-distribution-scroll]')
            await table.scrollIntoViewIfNeeded()
            await expect(
              page.locator('[data-model-distribution-scroll-hint]')
            ).toBeVisible()
            await expect(table.locator('thead th')).toHaveCount(5)
            await table.focus()
            for (let i = 0; i < 10; i++) await page.keyboard.press('ArrowRight')
            await expect
              .poll(() => table.evaluate((element) => element.scrollLeft))
              .toBeGreaterThan(0)
            await expect
              .poll(() =>
                table.evaluate((element) => {
                  const lastCell = element.querySelector(
                    'tbody tr td:last-child'
                  )!
                  return (
                    lastCell.getBoundingClientRect().right <=
                    element.getBoundingClientRect().right + 1
                  )
                })
              )
              .toBe(true)
            await expect(
              table.locator('tbody tr td:last-child').first()
            ).toHaveText('$2.00')
            await page.screenshot({
              path: testInfo.outputPath(
                `dashboard-table-end-${theme}-${width}.png`
              ),
              fullPage: true,
              animations: 'disabled',
            })
            await table.evaluate((element) =>
              element.scrollTo({ left: 0, behavior: 'instant' })
            )
          }
        }
        if (path === '/profile') {
          await expect(page.getByTestId('profile-join-date')).toHaveText(
            'Unavailable'
          )
          await expect(page.getByTestId('profile-member-duration')).toHaveText(
            'Unavailable'
          )
          await expect(page.getByTestId('profile-stat-balance')).toBeVisible()
          if (width < 768) {
            const hero = page.locator('[data-handdrawn="page-hero"]')
            const title = await hero.getByRole('heading').boundingBox()
            const subtitle = await hero.locator('p').boundingBox()
            expect(title).not.toBeNull()
            expect(subtitle).not.toBeNull()
            expect(subtitle!.width).toBeGreaterThan(300)
            expect(subtitle!.y).toBeGreaterThanOrEqual(title!.y + title!.height)
          }
        }
        if (path === '/activity') {
          await expect(page.locator('input[type=month]')).toBeVisible()
          await expect(
            page.locator('[aria-label="Check-in calendar"]')
          ).toBeVisible()
          await expect(page.locator('[data-checkin-action]')).toBeVisible()
          await expect(page.locator('.activity-entry__tag svg')).toHaveCount(2)
          await expect(page.locator('.activity-entry__tag')).not.toContainText([
            '🌾',
            '🎮',
          ])
        }
        if (path === '/invite') {
          const affiliate = page.locator('[data-invite-affiliate-card]')
          for (const [name, content] of [
            ['Copy code', 'NATIVE-AFF'],
            [
              'Copy link',
              `${new URL(page.url()).origin}/sign-up?aff=NATIVE-AFF`,
            ],
          ]) {
            const copy = affiliate.getByRole('button', { name, exact: true })
            await copy.scrollIntoViewIfNeeded()
            await expect(copy).toBeVisible()
            const bounds = await affiliate.boundingBox()
            const button = await copy.boundingBox()
            expect(bounds).not.toBeNull()
            expect(button).not.toBeNull()
            expect(button!.x).toBeGreaterThanOrEqual(bounds!.x)
            expect(button!.x + button!.width).toBeLessThanOrEqual(
              bounds!.x + bounds!.width
            )
            await copy.click()
            await expect
              .poll(() => page.evaluate(() => navigator.clipboard.readText()))
              .toBe(content)
          }
          await affiliate.scrollIntoViewIfNeeded()
          await page.screenshot({
            path: testInfo.outputPath(`invite-copy-${theme}-${width}.png`),
            fullPage: true,
            animations: 'disabled',
          })
          for (const text of await page
            .locator('[data-affiliate-transfer] p')
            .all()) {
            expect(await minTextContrast(text)).toBeGreaterThanOrEqual(4.5)
          }
          await page
            .getByRole('button', { name: 'Transfer to balance', exact: true })
            .click()
          const dialog = page.getByRole('dialog', {
            name: 'Transfer to balance',
          })
          await expect(dialog).toBeVisible()
          await dialog.getByRole('textbox').fill('0.5')
          await expect(
            dialog.getByRole('button', {
              name: 'Transfer to balance',
              exact: true,
            })
          ).toBeDisabled()
          await page.screenshot({
            path: testInfo.outputPath(`invite-transfer-${theme}-${width}.png`),
            fullPage: true,
            animations: 'disabled',
          })
          await page.keyboard.press('Escape')
          await expect(dialog).toBeHidden()
        }
        if (path === '/ops') {
          await expect(
            page.getByText('Go runtime memory', { exact: true })
          ).toBeVisible()
          await expect(
            page.getByText('Health score', { exact: true })
          ).toBeVisible()
          await expect(
            page.getByRole('region', { name: 'Active Tasks', exact: true })
          ).toBeVisible()
          const menu = page.locator('.ops-menu')
          const trigger = menu.locator('summary')
          await trigger.scrollIntoViewIfNeeded()
          const layout = await trigger.evaluate((element) => {
            const button = element.getBoundingClientRect()
            const icon = element.querySelector('svg')!.getBoundingClientRect()
            return {
              marker: getComputedStyle(element).listStyleType,
              center: Math.abs(
                button.y + button.height / 2 - icon.y - icon.height / 2
              ),
            }
          })
          expect(layout.marker).toBe('none')
          expect(layout.center).toBeLessThan(1)
          await trigger.focus()
          await page.keyboard.press('Enter')
          await expect(
            menu.getByRole('button', { name: 'Clean up logs', exact: true })
          ).toBeVisible()
          await page.screenshot({
            path: testInfo.outputPath(`ops-menu-${theme}-${width}.png`),
            fullPage: true,
            animations: 'disabled',
          })
          await page.keyboard.press('Escape')
          await expect(menu).not.toHaveAttribute('open')
        }
        expect(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth + 1
          ),
          path
        ).toBe(true)
        expect(fixture.errors, path).toEqual([])
        expect(fixture.unexpected, path).toEqual([])
        expect([...new Set(fixture.missingMessages)], path).toEqual([])
        const scroller = page.locator('.scroll-zone')
        const extent = await scroller.evaluate((element) => ({
          height: element.clientHeight,
          maximum: element.scrollHeight - element.clientHeight,
        }))
        const offsets = [0]
        if (path !== '/profile') {
          for (
            let offset = extent.height;
            offset < extent.maximum;
            offset += extent.height
          )
            offsets.push(offset)
          if (extent.maximum > 0) offsets.push(extent.maximum)
        }
        for (const [index, offset] of offsets.entries()) {
          await scroller.evaluate(
            (element, top) => element.scrollTo({ top, behavior: 'instant' }),
            offset
          )
          await page.screenshot({
            path: testInfo.outputPath(
              `${path.slice(1)}-${theme}-${width}-part${index + 1}.png`
            ),
            fullPage: true,
            animations: 'disabled',
          })
        }
      }
    })
  }
