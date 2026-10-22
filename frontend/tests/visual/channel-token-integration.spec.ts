import { expect, test, type Page } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import { nativeFixture } from './native-core-fixture'

type Channel = Record<string, unknown>
const generated = nativeFixture<{ data: { items: Channel[] } }>(
  'channel-multi-key-list.get.json'
).data.items
const single = generated.find((item) => item.name === 'contract-single')!
// Extra rows reuse the handler-generated single-key shape, so only the
// type-specific fields differ from real list output.
const channels: Channel[] = [
  ...generated,
  {
    ...single,
    id: 41,
    name: 'Vertex Fixture',
    type: 41,
    other: 'us-central1',
    settings: JSON.stringify({ vertex_key_type: 'json' }),
  },
  {
    ...single,
    id: 60,
    name: 'New API Fixture',
    type: 60,
    base_url: 'https://newapi.example',
  },
  {
    ...single,
    id: 61,
    name: 'Plugin Fixture',
    type: 61,
    base_url: 'https://plugin.example',
    setting: JSON.stringify({ task_plugin_key: 'video-plugin' }),
  },
  { ...single, id: 99, name: 'Unknown Fixture', type: 99 },
]

function row(page: Page, name: string) {
  return page
    .locator('tbody tr:visible, [data-channel-mobile-row]:visible')
    .filter({ hasText: name })
}

async function mockChannels(page: Page, saved: Channel[]) {
  const typeCounts: Record<string, number> = {}
  for (const item of channels)
    typeCounts[String(item.type)] = (typeCounts[String(item.type)] ?? 0) + 1
  await page.route(/\/api\/channel\/(?:\?.*)?$/, (route) => {
    if (route.request().method() === 'PUT') {
      saved.push(route.request().postDataJSON())
      return route.fulfill({ json: { success: true, data: null } })
    }
    return route.fulfill({
      json: {
        success: true,
        data: {
          items: channels,
          total: channels.length,
          page: 1,
          page_size: 20,
          type_counts: typeCounts,
        },
      },
    })
  })
}

async function editAndSave(page: Page, name: string) {
  await row(page, name)
    .locator('[aria-label="编辑渠道"]:visible')
    .first()
    .click()
  const dialog = page.getByRole('dialog', { name: '编辑渠道', exact: true })
  await expect(dialog).toBeVisible()
  await assertNoHorizontalOverflow(page)
  await dialog.getByRole('button', { name: '保存渠道', exact: true }).click()
  await expect(dialog).toHaveCount(0)
}

const viewports = [
  { width: 1440, height: 900 },
  { width: 390, height: 844 },
]

for (const theme of ['light', 'dark'] as const) {
  for (const viewport of viewports) {
    test(`${theme} ${viewport.width} channel key status and native types`, async ({
      page,
    }) => {
      await page.setViewportSize(viewport)
      await configureStablePage(page, { theme, authenticated: true })
      const saved: Channel[] = []
      await mockChannels(page, saved)
      await page.goto('/channels')
      await waitForStablePage(page)
      const chip = (name: string) =>
        row(page, name).locator('[data-channel-key-status]')
      await expect(chip('contract-multi-exhausted')).toHaveText(
        '0/2 个 Key 可用'
      )
      await expect(chip('contract-multi-partial')).toHaveText('1/3 个 Key 可用')
      await expect(chip('contract-multi-restored')).toHaveText(
        '2/2 个 Key 可用'
      )
      await expect(chip('contract-multi-new')).toHaveText('2/2 个 Key 可用')
      await expect(chip('contract-single')).toHaveCount(0)
      await expect(row(page, 'Unknown Fixture')).toContainText('未知类型 99')
      await assertNoHorizontalOverflow(page)

      await row(page, 'contract-multi-partial')
        .locator('[aria-label="渠道运行状态"]:visible')
        .first()
        .click()
      const runtime = page.getByRole('dialog', {
        name: 'contract-multi-partial',
        exact: true,
      })
      const disabledKeys = runtime.locator('[data-channel-disabled-keys] li')
      await expect(disabledKeys).toHaveCount(2)
      await expect(disabledKeys.nth(0)).toContainText('Key #2')
      await expect(disabledKeys.nth(0)).toContainText('手动禁用')
      await expect(disabledKeys.nth(1)).toContainText('Key #3')
      await expect(disabledKeys.nth(1)).toContainText('自动禁用')
      await assertNoHorizontalOverflow(page)
      await page.keyboard.press('Escape')
      await expect(runtime).toHaveCount(0)

      await editAndSave(page, 'Vertex Fixture')
      await editAndSave(page, 'New API Fixture')
      await editAndSave(page, 'Plugin Fixture')
      expect(saved.map((body) => body.id)).toEqual([41, 60, 61])
      expect(saved[0]).toMatchObject({ other: 'us-central1' })
      expect(saved[1]).not.toHaveProperty('setting')
      expect(saved[2]).toMatchObject({
        setting: JSON.stringify({ task_plugin_key: 'video-plugin' }),
      })

      await row(page, 'Unknown Fixture')
        .locator('[aria-label="编辑渠道"]:visible')
        .first()
        .click()
      const unknown = page.getByRole('dialog', {
        name: '编辑渠道',
        exact: true,
      })
      await expect(unknown).toContainText(
        '前端无法识别渠道类型 99，不能在此保存。'
      )
      await expect(
        unknown.getByRole('button', { name: '保存渠道', exact: true })
      ).toBeDisabled()
      await assertNoHorizontalOverflow(page)
      expect(saved).toHaveLength(3)
    })

    test(`${theme} ${viewport.width} token edit keeps expiry seconds`, async ({
      page,
    }) => {
      await page.setViewportSize(viewport)
      const { token } = await configureStablePage(page, {
        theme,
        authenticated: true,
      })
      // 2026-08-01 23:00:30 in Asia/Shanghai, the configured browser zone.
      const expiring = {
        ...token,
        expired_time: Date.UTC(2026, 7, 1, 15, 0, 30) / 1000,
      }
      const writes: Record<string, unknown>[] = []
      await page.route(/\/api\/token\/(?:search)?(?:\?.*)?$/, (route) => {
        if (route.request().method() === 'PUT') {
          writes.push(route.request().postDataJSON())
          return route.fulfill({
            json: { success: true, message: '', data: null },
          })
        }
        return route.fulfill({
          json: {
            success: true,
            message: '',
            data: { page: 1, page_size: 20, total: 1, items: [expiring] },
          },
        })
      })
      await page.goto('/keys')
      await waitForStablePage(page)
      const edit = page
        .getByRole('button', { name: '编辑令牌', exact: true })
        .filter({ visible: true })
        .first()
      const expiry = page.locator('[name="token-expiration"]')
      const confirm = page.getByRole('button', { name: '确认', exact: true })

      await edit.click()
      await expect(expiry).toHaveValue(/^2026-08-01T23:00:30(\.000)?$/)
      await assertNoHorizontalOverflow(page)
      await confirm.click()
      await expect(expiry).toBeHidden()
      expect(writes[0]?.expired_time).toBe(expiring.expired_time)

      await edit.click()
      await expiry.fill('2026-08-02T08:15:45')
      await confirm.click()
      await expect(expiry).toBeHidden()
      expect(writes[1]?.expired_time).toBe(
        Date.UTC(2026, 7, 2, 0, 15, 45) / 1000
      )
    })
  }
}
