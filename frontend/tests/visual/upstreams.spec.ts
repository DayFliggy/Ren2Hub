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
  list: { data: Array<{ name: string; site_meta: { balance: string } }> }
  newapi_keys: { data: Array<{ name: string }> }
  bindings: { data: Array<{ channel_id: number }> }
}
const account = fixture.list.data[0]!
const remoteKey = fixture.newapi_keys.data[0]!
const boundChannel = fixture.bindings.data[0]!.channel_id

const desktop = { width: 1440, height: 900 }
const mobile = { width: 390, height: 844 }

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
  size: { width: number; height: number },
  role: Role,
  path = '/upstreams'
) {
  await page.setViewportSize(size)
  await configureStablePage(page, {
    theme,
    authenticated: true,
    user: USERS[role],
  })
  await page.goto(path, { waitUntil: 'domcontentloaded' })
  await waitForStablePage(page)
}

/** Overflow inside the console scroll zone, where page content lives. */
async function assertZoneFits(page: Page) {
  await assertNoHorizontalOverflow(page)
  const zone = page.locator('[data-console-scroll-zone]')
  const overflow = await zone.evaluate(
    (element) => element.scrollWidth - element.clientWidth
  )
  expect(overflow).toBeLessThanOrEqual(1)
}

async function assertDialogFits(page: Page) {
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  // Every rendered box must stay inside the drawer; decorative pseudo
  // elements clipped by a scroll container are not boxes.
  const overflowing = await dialog.evaluate((root) => {
    const frame = root.getBoundingClientRect()
    return [...root.querySelectorAll<HTMLElement>('*')]
      .filter((element) => {
        const box = element.getBoundingClientRect()
        if (!box.width || !box.height) return false
        return box.left < frame.left - 1 || box.right > frame.right + 1
      })
      .map((element) => element.outerHTML.slice(0, 120))
  })
  expect(overflowing).toEqual([])
  expect(
    await dialog.evaluate((root) => root.scrollWidth - root.clientWidth)
  ).toBeLessThanOrEqual(1)
  await assertNoHorizontalOverflow(page)
}

function visibleText(page: Page, text: string) {
  return page.getByText(text, { exact: true }).filter({ visible: true }).first()
}

for (const theme of ['light', 'dark'] as const) {
  for (const [label, size] of [
    ['desktop', desktop],
    ['mobile', mobile],
  ] as const) {
    test(`${theme} ${label} upstream list and detail drawer`, async ({
      page,
    }, info) => {
      await open(page, theme, size, 'root')
      await expect(visibleText(page, account.name)).toBeVisible()
      await expect(
        page
          .getByText(`${account.site_meta.balance} 额度单位`)
          .filter({ visible: true })
          .first()
      ).toBeVisible()
      await expect(page.locator('[data-upstream-add]')).toBeEnabled()
      await assertZoneFits(page)
      await captureEvidence(page, {
        path: info.outputPath(`upstreams-${theme}-${label}.png`),
      })

      await visibleText(page, account.name).click()
      await expect(page).toHaveURL(/[?&]account=4/)
      await expect(page.locator('[data-upstream-groups]')).toContainText(
        'standard'
      )
      await assertDialogFits(page)

      await page.getByRole('tab', { name: '远端 Key' }).click()
      await expect(page.locator('[data-upstream-keys]')).toContainText(
        remoteKey.name
      )
      await expect(page.locator('[data-upstream-key-create]')).toBeEnabled()
      await assertDialogFits(page)

      await page.getByRole('tab', { name: '已关联渠道' }).click()
      await expect(page.getByRole('dialog')).toContainText(String(boundChannel))
      await expect(page.locator('[data-upstream-binding-stale]')).toHaveCount(0)
      await assertDialogFits(page)
      await captureEvidence(page, {
        path: info.outputPath(`upstreams-drawer-${theme}-${label}.png`),
      })
    })
  }
}

for (const [theme, label, size] of [
  ['light', 'desktop', desktop],
  ['dark', 'mobile', mobile],
] as const) {
  test(`${theme} ${label} default admin cannot add accounts or keys`, async ({
    page,
  }) => {
    await open(page, theme, size, 'admin', '/upstreams?account=4')
    await expect(page.locator('[data-upstream-add]')).toBeDisabled()
    await expect(
      page.locator('[data-upstream-create-key-channel]')
    ).toBeDisabled()
    await page.getByRole('tab', { name: '远端 Key' }).click()
    await expect(page.locator('[data-upstream-key-create]')).toBeDisabled()
    await assertDialogFits(page)
  })

  test(`${theme} ${label} read-only role sees data but no actions`, async ({
    page,
  }) => {
    await open(page, theme, size, 'read-only')
    await expect(visibleText(page, account.name)).toBeVisible()
    await expect(page.locator('[data-upstream-add]')).toBeDisabled()
    await page
      .getByRole('button', { name: `${account.name} 的操作` })
      .filter({ visible: true })
      .click()
    const menu = page.getByRole('menu')
    await expect(
      menu.getByRole('menuitem', { name: '分组与 Key' })
    ).toBeEnabled()
    for (const name of ['立即同步', '名称与启用', '重新授权', '删除账号'])
      await expect(menu.getByRole('menuitem', { name })).toBeDisabled()
    await page.keyboard.press('Escape')
    await assertZoneFits(page)
  })

  test(`${theme} ${label} ordinary user has no upstream page`, async ({
    page,
  }) => {
    await open(page, theme, size, 'user')
    await expect(page).not.toHaveURL(/\/upstreams/)
    await expect(
      page.locator('[data-console-nav-item="upstream-management"]')
    ).toHaveCount(0)
    await assertZoneFits(page)
  })
}

test('root sees the upstream entry in the console navigation', async ({
  page,
}) => {
  await open(page, 'light', desktop, 'root')
  await expect(
    page.locator('aside a[data-console-nav-item="upstream-management"]')
  ).toBeVisible()
})
