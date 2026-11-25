import { readFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

const fixture = JSON.parse(
  readFileSync(
    new URL('../fixtures/native/announcements.get.json', import.meta.url),
    'utf8'
  )
) as {
  user_list: {
    data: {
      items: Array<Record<string, unknown> & { id: number; title: string }>
      unread_count: number
    }
  }
  admin_list: {
    data: {
      items: Array<Record<string, unknown> & { id: number; title: string }>
      page: number
      page_size: number
      total: number
    }
  }
  read_status: {
    data: {
      items: Array<Record<string, unknown>>
      page: number
      page_size: number
      total: number
      total_users: number
      read_count: number
    }
  }
}

type Role = 'root' | 'admin' | 'read-only' | 'user'

const USERS: Record<Role, Record<string, unknown>> = {
  root: { role: 100 },
  admin: {
    role: 10,
    permissions: {
      admin_permissions: {
        announcement: { read: true, write: true },
      },
    },
  },
  'read-only': {
    role: 10,
    permissions: {
      admin_permissions: {
        announcement: { read: true, write: false },
      },
    },
  },
  user: { role: 1 },
}

async function setupAnnouncementRoutes(
  page: Page,
  options: { withUnreadPopup?: boolean } = {}
) {
  let userItems = structuredClone(fixture.user_list.data.items).map((item) =>
    !options.withUnreadPopup && item.notify_mode === 'popup'
      ? { ...item, read_at: 1785124000 }
      : item
  )
  let adminItems = structuredClone(fixture.admin_list.data.items)
  let nextId = 100

  await page.route(/\/api\/announcements(?:\?.*)?$/, (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    const unreadCount = userItems.filter((item) => item.read_at === null).length
    return route.fulfill({
      json: {
        success: true,
        message: '',
        data: { items: userItems, unread_count: unreadCount },
      },
    })
  })

  await page.route(/\/api\/announcements\/(\d+)\/read$/, (route) => {
    const id = Number(route.request().url().split('/').slice(-2)[0])
    const nowSec = 1785125000
    userItems = userItems.map((item) =>
      item.id === id ? { ...item, read_at: nowSec } : item
    )
    return route.fulfill({
      json: { success: true, message: '', data: { id, read_at: nowSec } },
    })
  })

  await page.route(/\/api\/announcements\/read-all$/, (route) => {
    const nowSec = 1785125000
    const updated = userItems.filter((item) => item.read_at === null).length
    userItems = userItems.map((item) => ({
      ...item,
      read_at: item.read_at ?? nowSec,
    }))
    return route.fulfill({
      json: { success: true, message: '', data: { updated } },
    })
  })

  await page.route(/\/api\/admin\/announcements(?:\?.*)?$/, (route) => {
    const method = route.request().method()
    if (method === 'GET') {
      return route.fulfill({
        json: {
          success: true,
          message: '',
          data: {
            items: adminItems,
            page: 1,
            page_size: 20,
            total: adminItems.length,
          },
        },
      })
    }
    if (method === 'POST') {
      const body = route.request().postDataJSON() as Record<string, unknown>
      const created = {
        id: nextId++,
        title: String(body.title ?? ''),
        content: String(body.content ?? ''),
        type: String(body.type ?? 'default'),
        status: String(body.status ?? 'draft'),
        notify_mode: String(body.notify_mode ?? 'silent'),
        pinned: Boolean(body.pinned),
        starts_at: body.starts_at ?? null,
        ends_at: body.ends_at ?? null,
        created_by: 101,
        updated_by: 101,
        created_at: 1785126000,
        updated_at: 1785126000,
        read_count: 0,
      }
      adminItems = [created, ...adminItems]
      return route.fulfill({
        json: { success: true, message: '', data: created },
      })
    }
    return route.fallback()
  })

  await page.route(
    /\/api\/admin\/announcements\/(\d+)\/read-status(?:\?.*)?$/,
    (route) => route.fulfill({ json: fixture.read_status })
  )

  await page.route(/\/api\/admin\/announcements\/(\d+)$/, (route) => {
    const method = route.request().method()
    const id = Number(route.request().url().split('/').pop())
    if (method === 'PUT') {
      const patch = route.request().postDataJSON() as Record<string, unknown>
      let updated: Record<string, unknown> | null = null
      adminItems = adminItems.map((item) => {
        if (item.id !== id) return item
        updated = { ...item, ...patch, updated_at: 1785127000 }
        return updated as typeof item
      })
      return route.fulfill({
        json: { success: true, message: '', data: updated },
      })
    }
    if (method === 'DELETE') {
      adminItems = adminItems.filter((item) => item.id !== id)
      return route.fulfill({
        json: { success: true, message: '', data: { deleted: true } },
      })
    }
    return route.fallback()
  })
}

for (const theme of ['light', 'dark'] as const satisfies VisualTheme[]) {
  test(`topbar announcement bell replaces search box and handles popup + detail (${theme})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await configureStablePage(page, { theme, user: USERS.root })
    await setupAnnouncementRoutes(page, { withUnreadPopup: true })

    await page.goto('/dashboard', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    // Visible topbar search trigger is gone; announcement bell is shown.
    await expect(page.locator('.topbar-nav .search-trigger')).toHaveCount(0)
    const trigger = page.locator('[data-announcement-trigger]')
    await expect(trigger).toBeVisible()
    await expect(page.locator('[data-announcement-badge]')).toHaveText('2')

    // Unread popup announcement opens automatically and marks read on dismiss.
    const dismiss = page.locator('[data-announcement-popup-dismiss]')
    await expect(dismiss).toBeVisible()
    await expect(
      page.locator('[data-announcement-detail-title]')
    ).toContainText('余额结算维护通知')
    await dismiss.click()
    await expect(dismiss).toHaveCount(0)
    await expect(page.locator('[data-announcement-badge]')).toHaveText('1')

    // Clicking the bell opens the list modal, clicking an item shows Markdown detail.
    await trigger.click()
    await expect(page.locator('[data-announcement-list-modal]')).toBeVisible()
    await page.locator('[data-announcement-item="11"]').click()
    await expect(page.locator('[data-announcement-detail-body]')).toContainText(
      '优化自动选渠与缓存复用统计'
    )
    await expect(page.locator('[data-announcement-badge]')).toHaveCount(0)

    await assertNoHorizontalOverflow(page)
  })

  test(`admin announcement management supports create, preview, read status and mobile layout (${theme})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await configureStablePage(page, { theme, user: USERS.admin })
    await setupAnnouncementRoutes(page)

    await page.goto('/announcements', { waitUntil: 'domcontentloaded' })
    await waitForStablePage(page)

    await expect(page.locator('[data-announcements-page]')).toBeVisible()
    await expect(
      page.locator('aside a[data-console-nav-item="announcement-management"]')
    ).toHaveAttribute('aria-current', 'page')

    // Open read status dialog.
    await page.locator('[data-announcement-reads="11"]').click()
    await expect(
      page.locator('[data-announcement-read-status-modal]')
    ).toBeVisible()
    await expect(
      page.locator('[data-announcement-read-status-modal]')
    ).toContainText('member.demo')
    await page.keyboard.press('Escape')

    // Create a new announcement with live Markdown preview.
    await page.locator('[data-announcement-create]').click()
    await page.locator('[data-announcement-title-input]').fill('测试新公告标题')
    await page
      .locator('[data-announcement-content-input]')
      .fill('**重点内容** 支持 Markdown')
    await page.locator('[data-announcement-submit]').click()
    await expect(page.locator('[data-announcements-page]')).toContainText(
      '测试新公告标题'
    )
    await assertNoHorizontalOverflow(page)

    // Switch to mobile viewport: mobile card list renders cleanly with 44px bell trigger.
    await page.setViewportSize({ width: 390, height: 844 })
    await expect(
      page.locator('[data-announcement-mobile-row]').first()
    ).toBeVisible()
    const bellBox = await page
      .locator('[data-announcement-trigger]')
      .boundingBox()
    expect(bellBox!.width).toBeGreaterThanOrEqual(44)
    expect(bellBox!.height).toBeGreaterThanOrEqual(44)
    await assertNoHorizontalOverflow(page)
  })
}

test('read-only admin can view announcements and read status without write actions', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'dark', user: USERS['read-only'] })
  await setupAnnouncementRoutes(page)
  await page.goto('/announcements', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  await expect(page.locator('[data-announcements-page]')).toBeVisible()
  await expect(page.locator('[data-announcement-create]')).toHaveCount(0)
  await expect(page.locator('[data-announcement-edit="11"]')).toHaveCount(0)
  await expect(page.locator('[data-announcement-preview="11"]')).toBeVisible()
  await expect(page.locator('[data-announcement-reads="11"]')).toBeVisible()
})

test('regular member can use topbar announcements but is redirected away from admin management', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await configureStablePage(page, { theme: 'light', user: USERS.user })
  await setupAnnouncementRoutes(page)
  await page.goto('/announcements', { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)

  await expect(page).not.toHaveURL(/\/announcements/)
  await page.locator('[data-announcement-trigger]').click()
  await expect(page.locator('[data-announcement-list-modal]')).toBeVisible()
  await expect(page.locator('[data-announcement-manage-link]')).toHaveCount(0)
})
