import { expect, test } from '@playwright/test'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
  captureEvidence,
} from './fixtures'

for (const [theme, width] of [
  ['light', 1440],
  ['dark', 390],
] as const) {
  test(`${theme} ${width} AWS and Vertex channel configuration`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 })
    await configureStablePage(page, { theme, authenticated: true })
    let channel = {
      id: 101,
      name: 'Provider Fixture',
      type: 33,
      lab: { id: 'aws', name: 'AWS', source: 'vendor' },
      status: 1,
      priority: 0,
      weight: 10,
      in_flight: 0,
      max_concurrency: 20,
      channel_ratio: '1',
      used_quota: 0,
      balance: 0,
      response_time: 0,
      test_time: 0,
      base_url: '',
      models: 'claude-opus-4-7',
      model_mapping: '',
      other: 'global',
      setting: '{}',
      settings: '{}',
    }
    let saved: Record<string, unknown> | undefined
    let created: typeof channel | undefined
    const createWrites: unknown[] = []
    await page.route(/\/api\/channel\/(?:\?.*)?$/, (route) => {
      if (route.request().method() === 'POST') {
        createWrites.push(route.request().postDataJSON())
        created = { ...channel, id: 102, name: 'Created Fixture', type: 1 }
        return route.fulfill({
          json: {
            success: true,
            message: '',
            data: {
              id: 102,
              ids: [102],
              results: [{ input_index: 0, channel_id: 102 }],
            },
          },
        })
      }
      if (route.request().method() === 'PUT') {
        saved = route.request().postDataJSON()
        return route.fulfill({ json: { success: true, data: null } })
      }
      return route.fulfill({
        json: {
          success: true,
          data: {
            items: created ? [created, channel] : [channel],
            total: created ? 2 : 1,
            page: 1,
            page_size: 20,
            type_counts: { [channel.type]: 1 },
          },
        },
      })
    })
    await page.goto('/channels')
    await waitForStablePage(page)
    await page.locator('[aria-label="编辑渠道"]:visible').first().click()
    let dialog = page.getByRole('dialog', { name: '编辑渠道', exact: true })
    const awsMode = dialog.locator('#channel-aws-api-type')
    await expect(awsMode).toHaveValue('invoke_model')
    await awsMode.focus()
    await expect(awsMode).toBeFocused()
    await awsMode.selectOption('mantle')
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: test.info().outputPath('aws-provider.png'),
      fullPage: true,
    })
    await dialog.getByRole('button', { name: '保存渠道', exact: true }).click()
    await expect(dialog).toHaveCount(0)
    expect(JSON.parse(String(saved!.settings)).aws_api_type).toBe('mantle')

    channel = {
      ...channel,
      type: 41,
      lab: { id: 'google', name: 'Google', source: 'vendor' },
      models: 'gemini-4-argon',
      setting: '{}',
      settings:
        '{"vertex_project_id":"existing-project","vertex_key_type":"api_key","future_provider_setting":{"keep":true}}',
    }
    saved = undefined
    await page.reload()
    await waitForStablePage(page)
    await page.locator('[aria-label="编辑渠道"]:visible').first().click()
    dialog = page.getByRole('dialog', { name: '编辑渠道', exact: true })
    const project = dialog.locator('input[name="channel-vertex-project-id"]')
    const version = dialog.getByRole('combobox', { name: 'Vertex API 版本' })
    await expect(version).toHaveValue('')
    await expect(version.locator('option')).toHaveCount(3)
    await version.selectOption('v1')
    await expect(version).toHaveValue('v1')
    await version.selectOption('v1beta1')
    await expect(project).toHaveValue('existing-project')
    await project.fill('updated-project')
    await dialog.locator('[name="channel-vertex-region"]').fill('eu')
    await page.keyboard.press('Tab')
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: test.info().outputPath('vertex-provider.png'),
      fullPage: true,
    })
    await dialog.getByRole('button', { name: '保存渠道', exact: true }).click()
    await expect(dialog).toHaveCount(0)
    expect(JSON.parse(String(saved!.settings)).vertex_project_id).toBe(
      'updated-project'
    )
    expect(saved!.other).toBe('eu')
    expect(JSON.parse(String(saved!.settings))).toMatchObject({
      vertex_api_version: 'v1beta1',
      future_provider_setting: { keep: true },
    })

    channel = {
      ...channel,
      settings: '{"vertex_api_version":"v1alpha"}',
    }
    saved = undefined
    await page.reload()
    await waitForStablePage(page)
    await page.locator('[aria-label="编辑渠道"]:visible').first().click()
    dialog = page.getByRole('dialog', { name: '编辑渠道', exact: true })
    const invalidVersion = dialog.getByRole('combobox', {
      name: 'Vertex API 版本',
    })
    await expect(invalidVersion).toHaveValue('')
    const saveChannel = dialog.getByRole('button', {
      name: '保存渠道',
      exact: true,
    })
    await expect(dialog.getByRole('alert')).toHaveCount(0)
    await expect(saveChannel).toBeEnabled()
    await saveChannel.click()
    await expect(dialog).toHaveCount(0)
    expect(JSON.parse(String(saved!.settings)).vertex_api_version).toBe('')

    await page.getByRole('button', { name: '新建渠道', exact: true }).click()
    const createDialog = page.getByRole('dialog', {
      name: '新建渠道',
      exact: true,
    })
    await createDialog
      .locator('[name="admin-channel-name"]')
      .fill('Created Fixture')
    await createDialog.locator('[name="admin-channel-key"]').fill('fixture-key')
    const models = createDialog.getByPlaceholder('选择模型或添加自定义模型', {
      exact: true,
    })
    await models.fill('fixture-model')
    await models.press('Enter')
    await createDialog
      .getByRole('button', { name: '保存渠道', exact: true })
      .click()
    await expect(createDialog).toHaveCount(0)
    expect(createWrites).toEqual([
      expect.objectContaining({
        mode: 'single',
        channel: expect.objectContaining({
          name: 'Created Fixture',
          key: 'fixture-key',
          models: 'fixture-model',
        }),
      }),
    ])
    await expect(page.getByText('渠道已创建', { exact: true })).toBeVisible()
    await expect(
      page
        .getByText('Created Fixture', { exact: true })
        .filter({ visible: true })
    ).toBeVisible()
    await assertNoHorizontalOverflow(page)
    await captureEvidence(page, {
      path: test.info().outputPath('channel-created.png'),
    })
  })
}
