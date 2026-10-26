import { expect, test, type Page } from '@playwright/test'

import {
  assertInteractiveCentersVisible,
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

async function prepareTickets(page: Page, theme: VisualTheme) {
  await configureStablePage(page, { theme, authenticated: true })
  await page.route('**/api/admin/tickets/agents', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          items: [
            {
              id: 1,
              username: 'visual.root',
              display_name: 'Visual Root',
              email: 'visual-root@ren2hub.dev',
            },
          ],
        },
      },
    })
  )
  const now = 1785124800
  const ticket = {
    id: 1,
    title: 'Production request investigation',
    status: 'replied',
    category: 'api',
    priority: 'normal',
    reply_count: 1,
    message_count: 2,
    last_reply_role: 'support',
    model_id: 'gpt-native',
    request_id: 'native-request',
    created: now,
    updated: now,
    assignee_id: null,
    assignee_name: '',
    assigned_at: 0,
    user: {
      id: 2,
      username: 'requester',
      display_name: 'Visual Requester',
      email: 'requester@example.test',
    },
    user_deleted: false,
  }
  await page.route(/\/api\/(?:admin\/)?tickets(?:\?.*)?$/, (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { items: [ticket], page: 1, page_size: 20, total: 1 },
      },
    })
  )
  await page.route(/\/api\/(?:admin\/)?tickets\/1$/, (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          ticket,
          messages: [
            {
              id: 1,
              role: 'user',
              content: 'Please investigate this request.',
              images: [],
              created: now,
            },
            {
              id: 2,
              role: 'support',
              content: 'We are checking the native request record.',
              images: [],
              created: now + 60,
            },
          ],
        },
      },
    })
  )
}

const viewports = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'mobile', width: 390, height: 844 },
] as const

for (const theme of ['light', 'dark'] as const satisfies VisualTheme[]) {
  for (const viewport of viewports) {
    test(`${theme} ${viewport.name} user tickets`, async ({ page }) => {
      await page.setViewportSize(viewport)
      await prepareTickets(page, theme)
      await page.goto('/tickets', { waitUntil: 'domcontentloaded' })
      await waitForStablePage(page)

      await expect(
        page.getByRole('button', {
          name: 'Production request investigation',
          exact: true,
        })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)

      await page
        .getByRole('button', {
          name: 'Production request investigation',
          exact: true,
        })
        .click()
      await waitForStablePage(page)

      await expect(page).toHaveURL(/\/tickets\/1$/)
      await expect(page.getByText('客服', { exact: true })).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)
    })

    test(`${theme} ${viewport.name} ticket management`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(viewport)
      await prepareTickets(page, theme)
      await page.goto('/ticket-management/1', {
        waitUntil: 'domcontentloaded',
      })
      await waitForStablePage(page)

      const workspace = page.getByLabel('工单处理区')
      if (viewport.name === 'desktop') {
        await expect(
          page.getByRole('heading', { name: '工单队列', exact: true })
        ).toBeVisible()
      }
      await expect(
        workspace.getByRole('heading', {
          name: 'Production request investigation',
          exact: true,
        })
      ).toBeVisible()
      await expect(
        workspace.getByText('Visual Requester', { exact: true })
      ).toBeVisible()
      await assertNoHorizontalOverflow(page)
      await assertInteractiveCentersVisible(page)
      await page.screenshot({
        path: testInfo.outputPath('ticket-management-selected.png'),
      })

      if (viewport.name === 'mobile') {
        await page.getByRole('button', { name: '返回列表' }).click()
        await expect(
          page.getByRole('complementary', { name: '工单队列' })
        ).toBeVisible()
        await assertNoHorizontalOverflow(page)
        await assertInteractiveCentersVisible(page)
      }
    })
  }
}
