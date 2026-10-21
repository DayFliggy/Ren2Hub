import { randomUUID } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { accounts, login, navigate } from './helpers'

interface Step {
  name: string
  method: string
  path: string
  status: number
  businessSuccess: boolean
  requestFields: string[]
  uiVerified: boolean
}

async function readNative<T>(page: Page, path: string): Promise<T> {
  return page.evaluate(async (endpoint) => {
    const modulePath = '/src/api/client.ts'
    const { api } = await import(modulePath)
    return api.get(endpoint)
  }, path)
}

async function mutation(
  page: Page,
  steps: Step[],
  name: string,
  method: string,
  path: string,
  action: () => Promise<unknown>
) {
  const waiting = page.waitForResponse(
    (response) =>
      response.request().method() === method &&
      new URL(response.url()).pathname === path
  )
  await action()
  const response = await waiting
  const body = (await response.json()) as Record<string, unknown>
  const input = response.request().postDataJSON() as Record<
    string,
    unknown
  > | null
  const step: Step = {
    name,
    method,
    path,
    status: response.status(),
    businessSuccess: body.success === true,
    requestFields: Object.keys(input ?? {}),
    uiVerified: false,
  }
  steps.push(step)
  expect(response.status(), name + ' HTTP status').toBe(200)
  expect(body.success, name + ' native success flag').toBe(true)
  return {
    body,
    input: input ?? {},
    verified: () => {
      step.uiVerified = true
    },
  }
}

async function securityProof(page: Page) {
  const dialog = page
    .getByRole('dialog')
    .filter({ has: page.locator('input[autocomplete="current-password"]') })
  await expect(dialog).toBeVisible()
  await dialog
    .locator('input[autocomplete="current-password"]')
    .fill(accounts.root.password)
  await dialog.getByRole('button', { name: '确认', exact: true }).click()
  await expect(dialog).toHaveCount(0)
}

async function report(
  page: Page,
  info: TestInfo,
  exercise: (steps: Step[]) => Promise<void>
) {
  const steps: Step[] = []
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.name))
  await page.addInitScript(() =>
    localStorage.setItem('ren2hub_locale', 'zh-CN')
  )
  await login(page, 'root')
  try {
    await exercise(steps)
    expect(errors).toEqual([])
    expect(steps.every((step) => step.uiVerified)).toBe(true)
  } finally {
    const path = info.outputPath('admin-native-actions.json')
    writeFileSync(
      path,
      JSON.stringify(
        { title: info.title, steps, pageErrorTypes: errors },
        null,
        2
      ),
      { mode: 0o600 }
    )
    await info.attach('admin-native-actions.json', {
      path,
      contentType: 'application/json',
    })
  }
}

test('administrator creates a user adjusts native quota and changes status', async ({
  page,
}, info) => {
  await report(page, info, async (steps) => {
    const username = 'live' + Date.now().toString(36)
    await navigate(page, '/users')
    await page.getByRole('button', { name: '新建用户', exact: true }).click()
    let dialog = page.getByRole('dialog')
    await dialog.locator('input[name="admin-user-username"]').fill(username)
    await dialog
      .locator('input[name="admin-user-display-name"]')
      .fill('Candidate user')
    await dialog
      .locator('input[name="admin-user-password"]')
      .fill('Local-' + randomUUID())
    const created = await mutation(
      page,
      steps,
      'create user',
      'POST',
      '/api/user/',
      () =>
        dialog.getByRole('button', { name: '保存用户', exact: true }).click()
    )
    expect(created.input.role).toBe(1)
    expect(created.input.max_concurrency).toBe(0)
    await expect(dialog).toHaveCount(0)
    await page.locator('input[name="admin-user-search"]').fill(username)
    const row = page.locator('tr').filter({ hasText: username })
    await expect(row).toBeVisible()
    const result = await readNative<{
      items: Array<{ id: number; quota: number; status: number }>
    }>(page, '/api/user/search?keyword=' + username)
    expect(result.items).toHaveLength(1)
    const user = result.items[0]!
    expect(Number.isSafeInteger(user.quota)).toBe(true)
    created.verified()

    for (const entry of [
      { label: '增加', mode: 'add', value: '2', quota: 1000000 },
      { label: '扣减', mode: 'subtract', value: '1', quota: 500000 },
    ]) {
      await row.getByRole('button', { name: '调整额度', exact: true }).click()
      dialog = page.getByRole('dialog')
      await dialog
        .getByRole('radio', { name: entry.label, exact: true })
        .click()
      await dialog
        .locator('input[name="admin-user-quota-amount"]')
        .fill(entry.value)
      const adjusted = await mutation(
        page,
        steps,
        'quota ' + entry.mode,
        'POST',
        '/api/user/manage',
        () =>
          dialog.getByRole('button', { name: '确认变更', exact: true }).click()
      )
      expect(adjusted.input.action).toBe('add_quota')
      expect(adjusted.input.mode).toBe(entry.mode)
      expect(adjusted.input.value).toBe(entry.quota)
      expect(typeof adjusted.input.operation_id).toBe('string')
      expect(adjusted.input).not.toHaveProperty('expected_version')
      await expect(dialog).toHaveCount(0)
      const stored = await readNative<{ quota: number }>(
        page,
        `/api/user/${user.id}`
      )
      const expected = user.quota + (entry.mode === 'add' ? 1000000 : 500000)
      expect(stored.quota).toBe(expected)
      await expect(
        row.getByRole('button', { name: '调整额度', exact: true })
      ).toBeEnabled()
      adjusted.verified()
    }
    for (const entry of [
      { label: '禁用用户', action: 'disable', status: 2, next: '启用用户' },
      { label: '启用用户', action: 'enable', status: 1, next: '禁用用户' },
    ]) {
      const changed = await mutation(
        page,
        steps,
        entry.action + ' user',
        'POST',
        '/api/user/manage',
        async () => {
          await row
            .getByRole('button', { name: entry.label, exact: true })
            .click()
          await securityProof(page)
        }
      )
      expect(changed.input.action).toBe(entry.action)
      await expect(
        row.getByRole('button', { name: entry.next, exact: true })
      ).toBeEnabled()
      expect(
        (await readNative<{ status: number }>(page, `/api/user/${user.id}`))
          .status
      ).toBe(entry.status)
      changed.verified()
    }
  })
})

test('administrator creates disables and deletes native redemption codes', async ({
  page,
}, info) => {
  await report(page, info, async (steps) => {
    const name = 'live-' + Date.now().toString(36)
    await navigate(page, '/redemption-codes')
    await page.getByRole('button', { name: '生成兑换码', exact: true }).click()
    let dialog = page.getByRole('dialog')
    await dialog.getByLabel('名称', { exact: true }).fill(name)
    await dialog.getByPlaceholder('10', { exact: true }).fill('1.25')
    const created = await mutation(
      page,
      steps,
      'create redemption',
      'POST',
      '/api/redemption/',
      () => dialog.getByRole('button', { name: '生成', exact: true }).click()
    )
    expect(created.input.quota).toBe(625000)
    expect(created.input.count).toBe(1)
    expect(created.input.expired_time).toBe(0)
    expect(created.input).not.toHaveProperty('amount')
    await expect(page.getByText('生成成功', { exact: true })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await page.locator('input[name="admin-redemption-search"]').fill(name)
    const row = page.locator('tr').filter({ hasText: name })
    await expect(row).toBeVisible()
    created.verified()
    const disabled = await mutation(
      page,
      steps,
      'disable redemption',
      'PUT',
      '/api/redemption/',
      () => row.getByRole('button', { name: '禁用兑换码', exact: true }).click()
    )
    expect(disabled.input.status).toBe(2)
    await expect(
      row.getByRole('button', { name: '启用兑换码', exact: true })
    ).toBeEnabled()
    disabled.verified()
    const id = disabled.input.id
    expect(Number.isSafeInteger(id)).toBe(true)
    await row.getByRole('button', { name: '删除兑换码', exact: true }).click()
    dialog = page.getByRole('dialog')
    const removed = await mutation(
      page,
      steps,
      'delete redemption',
      'DELETE',
      `/api/redemption/${id}`,
      () => dialog.getByRole('button', { name: '删除', exact: true }).click()
    )
    await expect(dialog).toHaveCount(0)
    await expect(row).toHaveCount(0)
    removed.verified()
  })
})

test('administrator creates edits and deletes a disabled local channel', async ({
  page,
}, info) => {
  await report(page, info, async (steps) => {
    const name = 'live-channel-' + Date.now().toString(36)
    await navigate(page, '/channels')
    await page.getByRole('button', { name: '新建渠道', exact: true }).click()
    let dialog = page.getByRole('dialog')
    await dialog.locator('input[name="admin-channel-name"]').fill(name)
    await dialog
      .locator('input[name="admin-channel-base-url"]')
      .fill('http://127.0.0.1:9')
    await dialog
      .locator('[name="admin-channel-key"]')
      .fill('local-unused-' + randomUUID())
    const models = dialog.getByPlaceholder('选择模型或添加自定义模型', {
      exact: true,
    })
    await models.fill('candidate-ui-only')
    await models.press('Enter')
    await dialog.getByRole('switch').first().uncheck()
    const created = await mutation(
      page,
      steps,
      'create disabled local channel',
      'POST',
      '/api/channel/',
      () =>
        dialog.getByRole('button', { name: '保存渠道', exact: true }).click()
    )
    expect(created.input.mode).toBe('single')
    const channelInput = created.input.channel as Record<string, unknown>
    expect(channelInput.status).toBe(2)
    expect(channelInput.base_url).toBe('http://127.0.0.1:9')
    expect(channelInput.models).toBe('candidate-ui-only')
    expect(channelInput.channel_ratio).toBe('1')
    expect(channelInput).not.toHaveProperty('group')
    await expect(dialog).toHaveCount(0)
    await page
      .getByPlaceholder('搜索名称、ID 或上游地址', { exact: true })
      .fill(name)
    let row = page
      .locator('tr')
      .filter({ hasText: name })
      .filter({
        has: page.getByRole('button', { name: '编辑渠道', exact: true }),
      })
    await expect(row).toBeVisible()
    created.verified()
    await row.getByRole('button', { name: '编辑渠道', exact: true }).click()
    dialog = page.getByRole('dialog')
    const updatedName = name + '-edit'
    await dialog.locator('input[name="admin-channel-name"]').fill(updatedName)
    await dialog.locator('input[name="channel-ratio"]').fill('1.25')
    const edited = await mutation(
      page,
      steps,
      'edit channel ratio and name',
      'PUT',
      '/api/channel/',
      () =>
        dialog.getByRole('button', { name: '保存渠道', exact: true }).click()
    )
    expect(edited.input.channel_ratio).toBe('1.25')
    expect(edited.input.name).toBe(updatedName)
    await expect(dialog).toHaveCount(0)
    row = page
      .locator('tr')
      .filter({ hasText: updatedName })
      .filter({
        has: page.getByRole('button', { name: '编辑渠道', exact: true }),
      })
    await expect(row).toBeVisible()
    edited.verified()
    await row.getByRole('button', { name: '删除渠道', exact: true }).click()
    dialog = page.getByRole('dialog')
    const removed = await mutation(
      page,
      steps,
      'delete channel',
      'DELETE',
      `/api/channel/${edited.input.id}`,
      () => dialog.getByRole('button', { name: '删除', exact: true }).click()
    )
    await expect(dialog).toHaveCount(0)
    await expect(row).toHaveCount(0)
    removed.verified()
  })
})

test('administrator saves and restores a harmless site setting', async ({
  page,
}, info) => {
  await report(page, info, async (steps) => {
    await navigate(page, '/system-settings/site/system-info')
    const input = page.locator('#setting-SystemName')
    await expect(input).toBeVisible()
    const original = await input.inputValue()
    const changed = original + ' Candidate'
    try {
      await input.fill(changed)
      const saved = await mutation(
        page,
        steps,
        'save site name',
        'PUT',
        '/api/option/',
        () =>
          page.getByRole('button', { name: '保存更改', exact: true }).click()
      )
      expect(saved.input.key).toBe('SystemName')
      expect(saved.input.value).toBe(changed)
      await expect(
        page.getByRole('button', { name: '保存更改', exact: true })
      ).toBeDisabled()
      await navigate(page, '/system-settings/site/notice')
      await navigate(page, '/system-settings/site/system-info')
      await expect(input).toHaveValue(changed)
      saved.verified()
    } finally {
      await input.fill(original)
      const restored = await mutation(
        page,
        steps,
        'restore site name',
        'PUT',
        '/api/option/',
        () =>
          page.getByRole('button', { name: '保存更改', exact: true }).click()
      )
      expect(restored.input.value).toBe(original)
      await expect(
        page.getByRole('button', { name: '保存更改', exact: true })
      ).toBeDisabled()
      await expect(input).toHaveValue(original)
      restored.verified()
    }
  })
})
