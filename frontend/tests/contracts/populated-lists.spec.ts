import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import { viewports } from './page-matrix'
import { populatedListFixture, populatedLists } from './populated-lists-fixture'
import { captureUI } from './ui-coverage'

for (const [size, viewport] of Object.entries(viewports)) {
  for (const theme of ['light', 'dark'] as const) {
    for (const locale of ['zh-CN', 'en'] as const) {
      for (const target of populatedLists) {
        test(`populated ${target.path} ${size}-${theme}-${locale}`, async ({
          page,
        }, info) => {
          await page.setViewportSize(viewport)
          const fixture = await configureStablePage(page, {
            theme,
            locale,
            clockStepMs: 0,
          })
          await populatedListFixture(page, target.path)
          const reads: string[] = []
          page.on('request', (request) => {
            if (request.method() === 'GET')
              reads.push(new URL(request.url()).pathname)
          })

          await page.goto(target.path)
          await expect(page).toHaveURL(new RegExp(`${target.path}$`))
          const rowText = page
            .getByText(target.rowText, {
              // Mobile relay cards combine platform/action/ID in one line.
              exact: !target.path.startsWith('/usage-logs/'),
            })
            .filter({ visible: true })
            .first()
          await expect(rowText).toBeVisible({ timeout: 30_000 })
          await page.waitForLoadState('networkidle')
          await waitForStablePage(page)
          await rowText.scrollIntoViewIfNeeded()
          await expect(rowText).toBeInViewport({ ratio: 0.5 })
          expect(reads).toContain(target.endpoint)
          // Session restoration is a POST; normal list rendering must not
          // perform any business mutation after that explicit bootstrap call.
          expect(
            fixture.writes.filter(
              (request) => request.path !== '/api/user/auth/refresh'
            )
          ).toEqual([])
          await assertNoHorizontalOverflow(page)
          await captureUI(page, info, 'populated-list', { compare: true })
          await waitForStablePage(page)
        })
      }
    }
  }
}
