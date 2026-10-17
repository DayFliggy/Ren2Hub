import { expect, test } from '@playwright/test'
import { nativeCoreFixture } from './native-core-fixture'
import { assertNoHorizontalOverflow, waitForStablePage } from './fixtures'

for (const [theme, width] of [
  ['light', 1440],
  ['dark', 390],
] as const) {
  test(`restored Chinese pages and client configuration ${theme} ${width}`, async ({
    page,
  }, testInfo) => {
    test.setTimeout(120_000)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.setViewportSize({ width, height: 1000 })
    const fixture = await nativeCoreFixture(page, theme, 'USD', {
      locale: 'zh-CN',
    })
    await page.route('**/api/token/7', (route) =>
      route.fulfill({
        json: { success: true, data: fixture.token },
      })
    )
    for (const path of [
      '/dashboard',
      '/profile',
      '/activity',
      '/invite',
      '/orders',
      '/ops',
      '/models',
      '/keys',
    ]) {
      await page.goto(path)
      await waitForStablePage(page)
      await assertNoHorizontalOverflow(page)
      expect(fixture.missingMessages).toEqual([])
      await page.screenshot({
        path: testInfo.outputPath(`${path.slice(1)}-zh.png`),
      })
    }
    await page
      .getByRole('button', { name: '客户端配置', exact: true })
      .filter({ visible: true })
      .click()
    const dialog = page.getByRole('dialog', { name: '客户端配置', exact: true })
    const preview = dialog.getByRole('textbox', { name: '配置预览' })
    await expect(dialog.getByRole('alert')).toContainText('有效的 HTTP')
    await dialog
      .getByRole('button', { name: '使用当前站点地址', exact: true })
      .click()
    await dialog.getByRole('button', { name: '生成配置', exact: true }).click()
    await expect(preview).toHaveValue(/env_key = "REN2HUB_API_KEY"/)
    await page.screenshot({ path: testInfo.outputPath('codex-zh.png') })
    await dialog.getByLabel('客户端', { exact: true }).selectOption('pi')
    await dialog
      .getByLabel('上下文上限（Token）', { exact: true })
      .fill('64000')
    await dialog.getByLabel('最大输出（Token）', { exact: true }).fill('8000')
    await dialog.getByLabel('推理能力', { exact: true }).selectOption('yes')
    await dialog.getByLabel('输入类型', { exact: true }).selectOption('image')
    await dialog.getByRole('button', { name: '生成配置', exact: true }).click()
    await expect(preview).toHaveValue(/\$\{REN2HUB_API_KEY\}/)
    await expect(dialog).toContainText('不代表免费')
    await assertNoHorizontalOverflow(page)
    await page.screenshot({ path: testInfo.outputPath('pi-zh.png') })
    expect(fixture.missingMessages).toEqual([])
    expect(fixture.errors).toEqual([])
    expect(fixture.unexpected).toEqual([])
    expect(
      fixture.writes.filter((entry) => !entry.path.endsWith('/refresh'))
    ).toEqual([])
  })
}
