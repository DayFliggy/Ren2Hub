import { expect, test } from '@playwright/test'
import { taskPluginMessages } from '../../src/i18n/taskPlugins'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import {
  MARKET_ORIGIN,
  PLUGIN_KEY,
  pluginSource,
  taskPluginFixture,
} from './task-plugin-fixture'
import { captureUI } from './ui-coverage'

const scenarios = [
  {
    key: 'desktop-light-zh',
    locale: 'zh-CN',
    theme: 'light',
    width: 1440,
    height: 900,
  },
  {
    key: 'mobile-dark-en',
    locale: 'en',
    theme: 'dark',
    width: 390,
    height: 844,
  },
] as const

for (const scenario of scenarios) {
  const copy = taskPluginMessages[scenario.locale]
  test(`task plugin installed details and sandbox ${scenario.key}`, async ({
    page,
  }, info) => {
    test.setTimeout(120_000)
    await page.setViewportSize(scenario)
    await configureStablePage(page, scenario)
    const fixture = await taskPluginFixture(page)
    await page.goto('/task-plugins')
    await waitForStablePage(page)
    await expect(
      page.getByText('UI task fixture', { exact: true })
    ).toBeVisible()
    await captureUI(page, info, 'plugin-list')
    if (scenario.width > 639) {
      await page
        .getByRole('radio', { name: copy.cardView, exact: true })
        .click()
      await captureUI(page, info, 'plugin-cards')
    }
    await page.getByRole('button', { name: copy.openMenu, exact: true }).click()
    await page
      .getByRole('menuitem', { name: copy.details, exact: true })
      .click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await expect(
      dialog.getByText('ui-video', { exact: true }).first()
    ).toBeVisible()
    await captureUI(page, info, 'plugin-overview')
    for (const [key, label] of [
      ['billing', copy.billingParams],
      ['source', copy.pluginSource],
      ['versions', copy.versionHistory],
    ] as const) {
      await dialog.getByRole('tab', { name: label, exact: true }).click()
      await captureUI(page, info, `plugin-${key}`)
    }
    await expect(dialog.getByText('1.0.0', { exact: true })).toBeVisible()
    await dialog
      .getByRole('tab', { name: copy.sourceDiff, exact: true })
      .click()
    await dialog
      .getByRole('combobox', { name: copy.selectCompare, exact: true })
      .click()
    await page.getByRole('option', { name: '1.0.0', exact: true }).click()
    await expect(
      dialog.getByRole('region', { name: copy.sourceDiff, exact: true })
    ).toContainText('1.0.0')
    await expect(
      dialog.getByRole('region', { name: copy.sourceDiff, exact: true })
    ).toContainText('2.0.0')
    await captureUI(page, info, 'plugin-source-diff')
    await dialog
      .getByRole('tab', { name: copy.pluginSandbox, exact: true })
      .click()
    await dialog
      .getByRole('textbox', { name: copy.argumentsJson, exact: true })
      .fill('[{"fixture":true}]')
    await dialog
      .getByRole('button', { name: copy.runDryRun, exact: true })
      .click()
    await expect(dialog.locator('pre')).toContainText('no_plugin_executed')
    expect(fixture.writes).toEqual([
      {
        path: `/api/plugin/task/${PLUGIN_KEY}/dryrun`,
        body: { hook: 'buildSubmitRequest', args: [{ fixture: true }] },
      },
    ])
    await captureUI(page, info, 'plugin-sandbox')
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await page
      .getByRole('switch', { name: copy.enableTaskPlugins, exact: true })
      .click()
    await expect(page.getByRole('dialog')).toContainText(copy.disableMasterDesc)
    await captureUI(page, info, 'plugin-disable-confirm')
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    expect(fixture.unexpected).toEqual([])
    await assertNoHorizontalOverflow(page)
  })

  test(`task plugin upload and marketplace review ${scenario.key}`, async ({
    page,
  }, info) => {
    test.setTimeout(120_000)
    await page.setViewportSize(scenario)
    await configureStablePage(page, scenario)
    const fixture = await taskPluginFixture(page)
    await page.goto('/task-plugins')
    await waitForStablePage(page)
    await page
      .getByRole('button', { name: copy.uploadPlugin, exact: true })
      .click()
    let dialog = page.getByRole('dialog')
    await dialog.locator('#task-plugin-file').setInputFiles({
      name: 'ui-fixture.js',
      mimeType: 'text/javascript',
      buffer: Buffer.from(pluginSource('2.1.0')),
    })
    await expect(dialog.locator('#task-plugin-source')).toHaveValue(
      pluginSource('2.1.0')
    )
    await expect(
      dialog.getByRole('region', { name: copy.connectionSettings, exact: true })
    ).toBeVisible()
    await captureUI(page, info, 'plugin-upload-preview')
    await dialog
      .getByRole('textbox', { name: copy.importFromUrl, exact: true })
      .fill(`${MARKET_ORIGIN}/${PLUGIN_KEY}/2.1.0/plugin.js`)
    await dialog.getByRole('button', { name: copy.fetch, exact: true }).click()
    await expect
      .poll(() => fixture.fetched)
      .toContain(`/${PLUGIN_KEY}/2.1.0/plugin.js`)
    await expect(
      dialog.getByRole('button', { name: copy.fetch, exact: true })
    ).toBeEnabled()
    await expect(dialog.locator('#task-plugin-source')).toHaveValue(
      pluginSource('2.1.0')
    )
    await captureUI(page, info, 'plugin-url-preview')
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await page.getByRole('tab', { name: copy.marketplace, exact: true }).click()
    await expect(page.locator('[data-marketplace-source]')).toContainText(
      'UI task fixture'
    )
    await captureUI(page, info, 'plugin-marketplace')
    await page
      .getByRole('button', { name: copy.manageSources, exact: true })
      .click()
    dialog = page.getByRole('dialog')
    await expect(
      dialog.getByRole('textbox', { name: copy.sourceName, exact: true })
    ).toHaveValue('UI fixture registry')
    await captureUI(page, info, 'plugin-sources')
    await dialog
      .getByRole('button', { name: copy.addSource, exact: true })
      .click()
    await expect(
      dialog.getByRole('button', { name: copy.save, exact: true })
    ).toBeDisabled()
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: copy.install, exact: true }).click()
    dialog = page.getByRole('dialog')
    await expect(
      dialog.getByRole('button', { name: copy.installAndEnable, exact: true })
    ).toBeEnabled()
    await captureUI(page, info, 'plugin-install-preview')
    for (const [key, label] of [
      ['diff', copy.versionDifferences],
      ['source', copy.fullSource],
      ['changelog', copy.changelog],
    ] as const) {
      await dialog.getByRole('tab', { name: label, exact: true }).click()
      if (key === 'changelog')
        await expect(
          dialog.getByText('Synthetic interface review with', { exact: false })
        ).toBeVisible()
      await captureUI(page, info, `plugin-install-${key}`)
    }
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    expect(fixture.writes).toEqual([])
    expect(fixture.unexpected).toEqual([])
    await assertNoHorizontalOverflow(page)
  })
}
