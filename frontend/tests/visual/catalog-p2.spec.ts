import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'
import { createModelMarketFixture } from '../fixtures/modelMarket'

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`native client configuration ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme, authenticated: true })
      const catalog = createModelMarketFixture()
      catalog.models[0]!.supported_endpoint_types.push('openai-response')
      catalog.supported_endpoints['openai-response'] = {
        path: '/v1/responses',
        method: 'POST',
      }
      await page.route('**/api/user/model-catalog**', (route) =>
        route.fulfill({ json: { success: true, data: catalog } })
      )
      await page.goto('/keys')
      await page
        .getByRole('button', { name: '客户端配置', exact: true })
        .filter({ visible: true })
        .click()
      const dialog = page.getByRole('dialog')
      await expect(dialog.getByLabel('配置预览')).toHaveValue(/REN2HUB_API_KEY/)
      await dialog.getByLabel('客户端', { exact: true }).selectOption('pi')
      await expect(dialog.getByLabel('配置预览')).toHaveCount(0)
      await dialog
        .getByLabel('上下文上限（Token）', { exact: true })
        .fill('32000')
      await dialog.getByLabel('最大输出（Token）', { exact: true }).fill('4096')
      await dialog.getByLabel('推理能力', { exact: true }).selectOption('no')
      await dialog.getByLabel('输入类型', { exact: true }).selectOption('text')
      await dialog
        .getByRole('button', { name: '生成配置', exact: true })
        .click()
      await expect(dialog.getByLabel('配置预览')).toHaveValue(/REN2HUB_API_KEY/)
      const configuration = JSON.parse(
        await dialog.getByLabel('配置预览').inputValue()
      )
      expect(configuration.providers.ren2hub).toMatchObject({
        apiKey: '${REN2HUB_API_KEY}',
        models: [
          {
            id: 'gpt-4.1',
            contextWindow: 32000,
            maxTokens: 4096,
            reasoning: false,
            input: ['text'],
          },
        ],
      })
      await expect(dialog).toContainText('价格由网关结算，配置不填写价格')
      const download = page.waitForEvent('download')
      await dialog.getByRole('button', { name: '下载', exact: true }).click()
      expect((await download).suggestedFilename()).toBe('ren2hub.models.json')
      await assertNoHorizontalOverflow(page)
      await page.keyboard.press('Escape')
      await expect(dialog).toHaveCount(0)
    })
  }
}
