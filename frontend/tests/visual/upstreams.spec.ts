import { readFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'

import {
  assertNoHorizontalOverflow,
  captureEvidence,
  configureStablePage,
  waitForStablePage,
  type VisualTheme,
} from './fixtures'

// The same contract fixture the Go controller test writes and verifies.
const fixture = JSON.parse(
  readFileSync(
    new URL('../fixtures/native/upstreams.get.json', import.meta.url),
    'utf8'
  )
) as {
  list: { data: Array<Record<string, unknown> & { name: string }> }
  newapi_keys: { data: Array<{ name: string }> }
  bindings: { data: Array<{ channel_id: number; remote_key_id: number }> }
}
const account = fixture.list.data[0]!
const remoteKey = fixture.newapi_keys.data[0]!
const boundChannel = fixture.bindings.data[0]!.channel_id
const boundKeyId = fixture.bindings.data[0]!.remote_key_id
// A second site that needs attention, built here so the shared contract
// fixture stays the backend's exact output.
const brokenSite = {
  ...structuredClone(account),
  id: 7,
  name: 'Sub2API 备用站',
  platform: 'sub2api',
  base_url: 'https://sub.example.invalid',
  has_proxy: false,
  status: 'reauth_required',
  last_error: 'upstream_unreachable',
  site_meta: { balance: '12.5', upstream_group: '', username: 'backup' },
  upstream_groups: [],
}

type Size = { width: number; height: number }
const SIZES = {
  desktop: { width: 1440, height: 900 },
  mobile: { width: 390, height: 844 },
  tablet: { width: 768, height: 1024 },
  laptop: { width: 1024, height: 768 },
  wide: { width: 1920, height: 1080 },
} satisfies Record<string, Size>

type Role = 'root' | 'admin' | 'read-only' | 'user'
const USERS: Record<Role, Record<string, unknown>> = {
  root: { role: 100 },
  // Backend defaults for Admin: every upstream action but sensitive_write.
  admin: {
    role: 10,
    permissions: {
      admin_permissions: {
        upstream: { read: true, operate: true, write: true },
        channel: { read: true },
      },
    },
  },
  'read-only': {
    role: 10,
    permissions: { admin_permissions: { upstream: { read: true } } },
  },
  user: { role: 1 },
}

async function open(
  page: Page,
  theme: VisualTheme,
  size: Size,
  role: Role,
  path = '/upstreams'
) {
  await page.setViewportSize(size)
  await configureStablePage(page, {
    theme,
    authenticated: true,
    user: USERS[role],
  })
  // Registered after the shared fixture, so these answers take precedence.
  await page.route(/\/api\/admin\/upstreams(?:\?.*)?$/, (route) =>
    route.fulfill({
      json: { success: true, message: '', data: [account, brokenSite] },
    })
  )
  // The contract fixture's key is unbound; bind a copy so the key → channel
  // link is exercised.
  await page.route(/\/api\/admin\/upstreams\/4\/keys(?:\?.*)?$/, (route) =>
    route.fulfill({
      json: {
        success: true,
        message: '',
        data: [{ ...remoteKey, id: boundKeyId }],
      },
    })
  )
  await page.route(/\/api\/admin\/upstreams\/7\/bindings(?:\?.*)?$/, (route) =>
    route.fulfill({ json: { success: true, message: '', data: [] } })
  )
  await page.goto(path, { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
}

async function overflowOf(page: Page, selector: string) {
  return page
    .locator(selector)
    .evaluate((element) => element.scrollWidth - element.clientWidth)
}

/** Page, console zone and both split columns stay within their width. */
async function assertFits(page: Page) {
  await assertNoHorizontalOverflow(page)
  expect(
    await overflowOf(page, '[data-console-scroll-zone]')
  ).toBeLessThanOrEqual(1)
  for (const selector of [
    '[data-upstream-site-list]',
    '[data-upstream-workspace]',
  ]) {
    const zone = page.locator(selector)
    if (!(await zone.isVisible())) continue
    // Name the boxes that stick out so a failure points at its cause.
    const culprits = await zone.evaluate((root) => {
      const right = root.getBoundingClientRect().right
      return [...root.querySelectorAll<HTMLElement>('*')]
        .filter(
          (element) =>
            element.getBoundingClientRect().right > right + 1 &&
            // Tabs scroll inside their own strip.
            !element.closest('[role="tablist"]')
        )
        .map((element) => element.outerHTML.slice(0, 160))
        .slice(0, 5)
    })
    expect(
      await overflowOf(page, selector),
      culprits.join('\n')
    ).toBeLessThanOrEqual(1)
  }
}

async function assertTouchTarget(page: Page, selector: string) {
  const box = await page
    .locator(selector)
    .filter({ visible: true })
    .first()
    .boundingBox()
  expect(box, selector).not.toBeNull()
  expect(box!.width, selector).toBeGreaterThanOrEqual(44)
  expect(box!.height, selector).toBeGreaterThanOrEqual(44)
}

function site(page: Page, name: string) {
  return page.locator('[data-upstream-site]').filter({ hasText: name })
}

function tab(page: Page, name: string) {
  return page.getByRole('tab', { name: new RegExp(`^${name}`) })
}

/** Number of tracks in the first remote key row. */
async function keyRowColumns(page: Page) {
  return page
    .locator('[data-upstream-key-row]')
    .first()
    .evaluate(
      (row) => getComputedStyle(row).gridTemplateColumns.split(' ').length
    )
}

async function accountMenu(page: Page) {
  await page
    .getByRole('button', { name: `${account.name} 的操作` })
    .filter({ visible: true })
    .click()
  return page.getByRole('menu')
}

for (const theme of ['light', 'dark'] as const) {
  test(`${theme} desktop split selects the first site and switches in place`, async ({
    page,
  }, info) => {
    await open(page, theme, SIZES.desktop, 'root')
    await expect(page).toHaveURL(/[?&]account=4(?:&|$)/)
    await expect(site(page, account.name)).toHaveAttribute(
      'aria-current',
      'page'
    )
    await expect(page.locator('[data-upstream-balance]')).toHaveText(
      `${(account.site_meta as { balance: string }).balance} 额度单位`
    )
    await expect(page.locator('[data-upstream-keys]')).toContainText(
      remoteKey.name
    )
    await expect(page.locator('[data-upstream-add]')).toBeEnabled()
    await expect(
      site(page, brokenSite.name).locator('[data-upstream-site-alert]')
    ).toBeVisible()
    await assertFits(page)
    await captureEvidence(page, {
      path: info.outputPath(`upstreams-${theme}-desktop.png`),
    })

    await page.locator('[data-upstream-key-channel]').first().click()
    await expect(page).toHaveURL(/[?&]tab=bindings/)
    await expect(
      page.locator(`[data-upstream-binding="${boundChannel}"]`)
    ).toHaveAttribute('data-highlighted', 'true')
    await assertFits(page)

    await site(page, brokenSite.name).click()
    await expect(page).toHaveURL(
      /account=7&tab=bindings|tab=bindings&account=7/
    )
    const banner = page.locator('[data-upstream-banner]')
    await expect(banner).toContainText('上游凭据已失效，需要重新授权')
    await expect(banner.locator('[data-upstream-alert-action]')).toBeEnabled()
    await tab(page, '远端 Key').click()
    await expect(page.locator('[data-upstream-keys-inactive]')).toBeVisible()
    await assertFits(page)
    await captureEvidence(page, {
      path: info.outputPath(`upstreams-${theme}-desktop-alert.png`),
    })

    await site(page, account.name).click()
    await tab(page, '分组与订阅').click()
    await expect(page.locator('[data-upstream-groups]')).toContainText(
      'standard'
    )
    await assertFits(page)
  })

  test(`${theme} mobile shows the list first and opens one site at a time`, async ({
    page,
  }, info) => {
    await open(page, theme, SIZES.mobile, 'root')
    await expect(page).not.toHaveURL(/account=/)
    await expect(page.locator('[data-upstream-workspace]')).toHaveCount(0)
    await assertTouchTarget(page, '[data-upstream-refresh]')
    await assertTouchTarget(page, '[data-upstream-add]')
    await assertFits(page)
    await captureEvidence(page, {
      path: info.outputPath(`upstreams-${theme}-mobile-list.png`),
    })

    await site(page, account.name).click()
    await expect(page).toHaveURL(/[?&]account=4/)
    await expect(page.locator('[data-upstream-site-list]')).toBeHidden()
    await expect(page.locator('[data-upstream-keys]')).toContainText(
      remoteKey.name
    )
    expect(await keyRowColumns(page)).toBe(3)
    await assertFits(page)
    await captureEvidence(page, {
      path: info.outputPath(`upstreams-${theme}-mobile-workspace.png`),
    })

    await page.locator('[data-upstream-back]').click()
    await expect(page).not.toHaveURL(/account=/)
    await expect(site(page, account.name)).toBeVisible()
  })

  test(`${theme} tablet keeps 44px touch targets`, async ({ page }, info) => {
    await open(page, theme, SIZES.tablet, 'root', '/upstreams?account=4')
    await expect(page.locator('[data-upstream-workspace]')).toBeVisible()
    for (const selector of [
      '[data-upstream-back]',
      '[data-upstream-sync]',
      '[data-upstream-actions] button[aria-haspopup="menu"]',
    ])
      await assertTouchTarget(page, selector)
    await assertFits(page)
    await captureEvidence(page, {
      path: info.outputPath(`upstreams-${theme}-tablet.png`),
    })

    await page.locator('[data-upstream-back]').click()
    await assertTouchTarget(page, '[data-upstream-refresh]')
    await assertTouchTarget(page, '[data-upstream-add]')
    await assertFits(page)
  })

  test(`${theme} 1024 with the sidebar open lays keys out as cards`, async ({
    page,
  }, info) => {
    await open(page, theme, SIZES.laptop, 'root')
    await expect(
      page.locator('aside[data-handdrawn="navigation"]')
    ).toBeVisible()
    await expect(page.locator('[data-upstream-site-list]')).toBeVisible()
    await expect(page.locator('[data-upstream-keys]')).toContainText(
      remoteKey.name
    )
    expect(await keyRowColumns(page)).toBe(3)
    await assertFits(page)
    await captureEvidence(page, {
      path: info.outputPath(`upstreams-${theme}-1024.png`),
    })
  })

  test(`${theme} 1920 lays keys out as table rows`, async ({ page }, info) => {
    await open(page, theme, SIZES.wide, 'root')
    await expect(page.locator('[data-upstream-keys]')).toContainText(
      remoteKey.name
    )
    expect(await keyRowColumns(page)).toBe(7)
    await assertFits(page)
    await captureEvidence(page, {
      path: info.outputPath(`upstreams-${theme}-1920.png`),
    })
  })

  test(`${theme} desktop fits at 200% root font size`, async ({
    page,
  }, info) => {
    await open(page, theme, SIZES.desktop, 'root')
    await page.evaluate(() => {
      document.documentElement.style.fontSize = '200%'
    })
    await expect(page.locator('[data-upstream-keys]')).toContainText(
      remoteKey.name
    )
    await assertFits(page)
    await captureEvidence(page, {
      path: info.outputPath(`upstreams-${theme}-font-200.png`),
    })
  })
}

for (const [theme, label, size] of [
  ['light', 'desktop', SIZES.desktop],
  ['dark', 'mobile', SIZES.mobile],
] as const) {
  test(`${theme} ${label} default admin cannot add accounts or keys`, async ({
    page,
  }) => {
    await open(page, theme, size, 'admin', '/upstreams?account=4')
    await expect(page.locator('[data-upstream-add]')).toBeDisabled()
    await expect(
      page.locator('[data-upstream-create-key-channel]')
    ).toBeDisabled()
    await expect(page.locator('[data-upstream-key-create]')).toBeDisabled()
    await expect(
      page.locator('[data-upstream-sync]').filter({ visible: true })
    ).toBeEnabled()
    await assertFits(page)
  })

  test(`${theme} ${label} read-only role sees data but no actions`, async ({
    page,
  }) => {
    await open(page, theme, size, 'read-only', '/upstreams?account=4')
    await expect(page.locator('[data-upstream-keys]')).toContainText(
      remoteKey.name
    )
    await expect(page.locator('[data-upstream-add]')).toBeDisabled()
    await expect(
      page.locator('[data-upstream-sync]').filter({ visible: true })
    ).toBeDisabled()
    const menu = await accountMenu(page)
    for (const name of ['名称与启用', '重新授权', '地址与代理', '删除账号'])
      await expect(menu.getByRole('menuitem', { name })).toBeDisabled()
    await expect(
      menu.getByRole('menuitem', { name: '分组与 Key' })
    ).toHaveCount(0)
    await page.keyboard.press('Escape')
    await assertFits(page)
  })

  test(`${theme} ${label} ordinary user has no upstream page`, async ({
    page,
  }) => {
    await open(page, theme, size, 'user')
    await expect(page).not.toHaveURL(/\/upstreams/)
    await expect(
      page.locator('[data-console-nav-item="upstream-management"]')
    ).toHaveCount(0)
    await assertFits(page)
  })
}

test('root sees the upstream entry in the console navigation', async ({
  page,
}) => {
  await open(page, 'light', SIZES.desktop, 'root')
  await expect(
    page.locator('aside a[data-console-nav-item="upstream-management"]')
  ).toBeVisible()
})
