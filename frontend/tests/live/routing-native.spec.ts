import { writeFileSync } from 'node:fs'
import { expect, test } from '@playwright/test'
import { login, navigate } from './helpers'

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 390, height: 844 },
]) {
  test(`native private routing saves empty manual and automatic policies at ${viewport.width}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(viewport)
    await page.addInitScript(() =>
      localStorage.setItem('ren2hub_locale', 'zh-CN')
    )
    const session = await login(page, 'user')
    const headers = { Authorization: `Bearer ${session.access_token}` }
    const name = `live-routing-${Date.now()}`
    const created = await page.request.post('/api/token/', {
      headers,
      data: {
        name,
        type: 'manual',
        remain_quota: 1,
        unlimited_quota: false,
        expired_time: -1,
        model_limits_enabled: false,
        model_limits: '',
        allow_ips: '',
        allow_user_quota_query: false,
      },
    })
    expect((await created.json()).success).toBe(true)
    const list = await page.request.get('/api/token/?p=1&page_size=100', {
      headers,
    })
    const matches = (await list.json()).data.items.filter(
      (item: { name: string }) => item.name === name
    )
    expect(matches).toHaveLength(1)
    const tokenID = matches[0].id as number
    try {
      await navigate(page, `/keys/${tokenID}/routing`)
      await page.getByRole('radio', { name: '手动路由', exact: true }).check()
      await page
        .getByRole('textbox', { name: '渠道倍率上限' })
        .fill('0.750000000000000001')
      const save = page.getByRole('button', { name: '保存', exact: true })
      const write = () =>
        page.waitForResponse(
          (response) =>
            response.request().method() === 'PUT' &&
            new URL(response.url()).pathname === `/api/token/${tokenID}/routing`
        )
      const manualResponse = write()
      await save.click()
      const manual = (await (await manualResponse).json()).data
      expect(manual.mode).toBe('manual')
      expect(manual.max_channel_ratio).toBe('0.750000000000000001')
      expect(manual.entries).toEqual([])
      await expect(save).toBeEnabled()
      await page.getByRole('radio', { name: '自动路由', exact: true }).check()
      await page.getByRole('textbox', { name: '渠道倍率上限' }).fill('')
      const autoResponse = write()
      await save.click()
      const auto = (await (await autoResponse).json()).data
      expect(auto.mode).toBe('auto')
      expect(auto.max_channel_ratio).toBeNull()
      expect(auto.entries).toEqual([])
      expect(auto.version).toBe(manual.version + 1)
      await page
        .getByRole('textbox', { name: '模型', exact: true })
        .fill('local-preview-without-channel')
      const previewResponse = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname ===
          `/api/token/${tokenID}/routing/preview`
      )
      await page
        .getByRole('button', { name: '预览已保存路由', exact: true })
        .click()
      const preview = await (await previewResponse).json()
      expect(preview.success).toBe(true)
      await expect(
        page.getByText(`已保存版本 ${auto.version}`, { exact: true })
      ).toBeVisible()
      await page.screenshot({
        path: testInfo.outputPath('native-routing-empty-policy.png'),
        fullPage: true,
      })
      writeFileSync(
        testInfo.outputPath('routing-ui-result.json'),
        JSON.stringify({
          manual_version: manual.version,
          automatic_version: auto.version,
          exact_ratio: manual.max_channel_ratio,
          empty_entries: true,
          local_preview_only: true,
        }),
        { mode: 0o600 }
      )
    } finally {
      const removed = await page.request.delete(`/api/token/${tokenID}`, {
        headers,
      })
      expect((await removed.json()).success).toBe(true)
    }
  })
}
