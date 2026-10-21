import { expect, test } from '@playwright/test'
import { login, navigate } from './helpers'

for (const theme of ['light', 'dark'] as const) {
  for (const viewport of [
    { name: 'desktop', width: 1440, height: 900 },
    { name: 'mobile', width: 390, height: 844 },
  ] as const) {
    test(`root ${theme} ${viewport.name} reaches Data Export settings`, async ({
      page,
    }) => {
      await page.setViewportSize(viewport)
      await page.emulateMedia({ colorScheme: theme })
      await page.addInitScript((selectedTheme) => {
        localStorage.setItem('ren2hub_theme_mode', selectedTheme)
        localStorage.setItem('ren2hub_locale', 'zh-CN')
      }, theme)
      const session = await login(page, 'root')
      const settings = page.waitForResponse(
        (response) =>
          response.request().method() === 'GET' &&
          new URL(response.url()).pathname === '/api/option/'
      )
      await navigate(page, '/system-settings/content/dashboard')
      await settings
      await expect(page).toHaveURL(/\/system-settings\/content\/dashboard$/)
      const section = page.locator('[data-handdrawn="surface"]')
      await expect(
        section.getByRole('heading', { name: '数据仪表盘', exact: true })
      ).toBeVisible()
      const enabled = section.getByRole('switch', {
        name: '启用数据导出',
        exact: true,
      })
      const interval = section.getByRole('spinbutton', {
        name: '数据刷新间隔（分钟）',
        exact: true,
      })
      const defaultRange = section.getByRole('combobox', {
        name: '默认时间范围',
        exact: true,
      })
      await expect(enabled).toBeVisible()
      await expect(interval).toBeVisible()
      await expect(defaultRange).toBeVisible()

      if (theme === 'light' && viewport.name === 'desktop') {
        const original = Number(await interval.inputValue())
        const changed = original === 6 ? 7 : 6
        const authorization = {
          Authorization: `Bearer ${session.access_token}`,
        }
        const save = section.getByRole('button', {
          name: '保存更改',
          exact: true,
        })
        await interval.fill(String(changed))
        const update = page.waitForResponse(
          (response) =>
            response.request().method() === 'PUT' &&
            new URL(response.url()).pathname === '/api/option/' &&
            response.request().postDataJSON()?.key === 'DataExportInterval'
        )
        await save.click()
        expect((await (await update).json()).success).toBe(true)
        await expect(interval).toHaveValue(String(changed))
        const restore = await page.request.put('/api/option/', {
          headers: authorization,
          data: { key: 'DataExportInterval', value: String(original) },
        })
        expect(restore.status()).toBe(200)
        expect((await restore.json()).success).toBe(true)
        await page.reload({ waitUntil: 'networkidle' })
        await expect(page).toHaveURL(/\/system-settings\/content\/dashboard$/)
        await expect(
          page.getByRole('spinbutton', {
            name: '数据刷新间隔（分钟）',
            exact: true,
          })
        ).toHaveValue(String(original))
      }
    })
  }
}
