import { readFile } from 'node:fs/promises'
import { expect, test, type Locator } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

// Response of a real export rejected at the global concurrency limit.
const busyFixture = new URL(
  '../fixtures/native/log-export-busy.json',
  import.meta.url
)

async function expectUnclipped(locator: Locator) {
  await expect(locator).toBeVisible()
  await expect(locator).toBeInViewport({ ratio: 1 })
  const visible = await locator.evaluate((element) => {
    const rect = element.getBoundingClientRect()
    for (
      let ancestor = element.parentElement;
      ancestor;
      ancestor = ancestor.parentElement
    ) {
      const style = getComputedStyle(ancestor)
      const bounds = ancestor.getBoundingClientRect()
      const left = bounds.left + ancestor.clientLeft
      const top = bounds.top + ancestor.clientTop
      if (
        /auto|scroll|hidden|clip/.test(style.overflowX) &&
        (rect.left < left - 1 || rect.right > left + ancestor.clientWidth + 1)
      )
        return false
      if (
        /auto|scroll|hidden|clip/.test(style.overflowY) &&
        (rect.top < top - 1 || rect.bottom > top + ancestor.clientHeight + 1)
      )
        return false
    }
    // Sample inside rounded corners; the full rectangle is checked above.
    const inset = Math.min(16, rect.width / 4, rect.height / 4)
    const points = [
      [rect.left + inset, rect.top + inset],
      [rect.right - inset, rect.top + inset],
      [rect.left + inset, rect.bottom - inset],
      [rect.right - inset, rect.bottom - inset],
      [rect.left + rect.width / 2, rect.top + rect.height / 2],
    ]
    return points.every(([x, y]) =>
      element.contains(document.elementFromPoint(x, y))
    )
  })
  expect(visible).toBe(true)
}

const row = {
  id: 1,
  type: 2,
  token_id: 7,
  token_name: 'export-fixture',
  model_name: 'fixture-model',
  channel_name: 'channel',
  quota: Number.MAX_SAFE_INTEGER,
  prompt_tokens: 1,
  completion_tokens: 2,
  use_time: 1,
  is_stream: true,
  content: '=untrusted',
  other: '{}',
  created_at: 1785123000,
}
for (const [theme, mobile] of [
  ['light', false],
  ['dark', true],
] as const) {
  test(`log export ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
    )
    await configureStablePage(page, { theme })
    // Route fulfilment sends a whole body at once. To show a running export,
    // fetch answers it with a few rows and keeps the stream open until the
    // request is aborted.
    await page.addInitScript((line: string) => {
      const native = window.fetch.bind(window)
      window.fetch = (input, init) => {
        const url = input instanceof Request ? input.url : String(input)
        const holdExport = (window as { holdExport?: boolean }).holdExport
        if (!holdExport || !url.includes('/api/log/self/export'))
          return native(input, init)
        const signal =
          init?.signal ?? (input instanceof Request ? input.signal : null)
        const body = new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(
              new TextEncoder().encode(`${line}\n${line}\n${line}\n`)
            )
            signal?.addEventListener('abort', () =>
              controller.error(new DOMException('Aborted', 'AbortError'))
            )
          },
        })
        return Promise.resolve(
          new Response(body, {
            headers: { 'Content-Type': 'application/x-ndjson' },
          })
        )
      }
    }, JSON.stringify(row))
    let exportURL = ''
    let fail = false
    let busyServer = false
    // The fixture file is pretty-printed; the server writes compact JSON.
    const busy = JSON.stringify(JSON.parse(await readFile(busyFixture, 'utf8')))
    await page.route('**/api/log/self/export?*', (route) => {
      exportURL = route.request().url()
      if (busyServer)
        return route.fulfill({
          contentType: 'application/json',
          body: busy,
        })
      return route.fulfill({
        contentType: 'application/x-ndjson',
        body:
          JSON.stringify(row) +
          '\n' +
          JSON.stringify(
            fail
              ? { success: false, message: 'Export unavailable' }
              : { success: true, data: { count: 1 } }
          ),
      })
    })
    await Promise.all([
      page.waitForResponse('**/api/log/self?*'),
      page.goto('/logs'),
    ])
    await page.getByRole('button', { name: '导出', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '导出数据' })
    await expect(
      dialog.getByText('沿用当前筛选条件；未选日期时导出全部日志。', {
        exact: true,
      })
    ).toBeVisible()
    await dialog.getByRole('button', { name: '选择日期范围' }).click()
    const calendar = dialog.getByRole('dialog', { name: '选择日期范围' })
    await expect(calendar).toBeVisible()
    await expectUnclipped(calendar)
    await expectUnclipped(calendar.getByText('2026年7月', { exact: true }))
    await expectUnclipped(calendar.getByText('周一', { exact: true }).first())
    await expectUnclipped(calendar.getByText('周日', { exact: true }).first())
    await expectUnclipped(
      calendar.locator('[data-date-key="2026-07-01"][data-in-month="true"]')
    )
    await expectUnclipped(
      calendar.locator('[data-date-key="2026-07-31"][data-in-month="true"]')
    )
    await expectUnclipped(
      calendar.getByRole('button', { name: '确定', exact: true })
    )
    await expectUnclipped(
      dialog.getByRole('button', { name: '确认', exact: true })
    )
    await assertNoHorizontalOverflow(page)
    if (process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS === 'true') {
      await page.screenshot({
        path: testInfo.outputPath('export-calendar.png'),
      })
    }
    await calendar
      .locator('[data-date-key="2026-07-01"][data-in-month="true"]')
      .click()
    await calendar
      .locator('[data-date-key="2026-07-31"][data-in-month="true"]')
      .click()
    await expect(calendar).not.toBeVisible()
    // With reduced motion the calendar sits in place from its first frame,
    // before Vue removes the enter class, so keyboard scrolling is exact.
    const firstFrame = await dialog
      .getByRole('button', { name: '选择日期范围' })
      .evaluate(async (trigger: HTMLElement) => {
        trigger.click()
        for (let tick = 0; tick < 3; tick++) await Promise.resolve()
        const panel = document.querySelector('.drp-pop-enter-from')
        return panel ? getComputedStyle(panel).transform : 'not entering'
      })
    expect(firstFrame).toBe('none')
    await page.keyboard.press('ArrowRight')
    await expect(
      calendar.locator('[data-date-key="2026-07-02"][data-in-month="true"]')
    ).toBeFocused()
    // A shorter window must scroll focused dates and actions into view.
    await page.setViewportSize(
      mobile ? { width: 390, height: 600 } : { width: 1440, height: 500 }
    )
    const scrollBody = dialog.locator('.subtle-scroll').first()
    expect(
      await scrollBody.evaluate(
        (element) => element.scrollHeight > element.clientHeight
      )
    ).toBe(true)
    for (let week = 0; week < 4; week++) await page.keyboard.press('ArrowDown')
    const monthEnd = calendar.locator(
      '[data-date-key="2026-07-30"][data-in-month="true"]'
    )
    await expect(monthEnd).toBeFocused()
    await expectUnclipped(monthEnd)
    await page.keyboard.press('Tab')
    await page.keyboard.press('Tab')
    const apply = calendar.getByRole('button', { name: '确定', exact: true })
    await expect(apply).toBeFocused()
    await expectUnclipped(apply)
    expect(
      await scrollBody.evaluate((element) => element.scrollTop)
    ).toBeGreaterThan(0)
    await page.keyboard.press('Escape')
    await expect(calendar).not.toBeVisible()
    await expect(dialog).toBeVisible()
    await expect(
      dialog.getByRole('button', { name: '选择日期范围' })
    ).toBeFocused()
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
    )
    await assertNoHorizontalOverflow(page)
    const download = page.waitForEvent('download')
    await dialog.getByRole('button', { name: '确认', exact: true }).click()
    const file = await download
    expect(file.suggestedFilename()).toBe('ren2hub-logs-20260701-20260731.csv')
    expect(new URL(exportURL).searchParams.has('group')).toBe(false)
    expect(new URL(exportURL).searchParams.get('start_timestamp')).toBe(
      String(new Date('2026-07-01T00:00:00+08:00').getTime() / 1000)
    )
    expect(new URL(exportURL).searchParams.get('end_timestamp')).toBe(
      String(new Date('2026-07-31T23:59:59+08:00').getTime() / 1000)
    )
    const path = await file.path()
    expect(path).not.toBeNull()
    const csv = await readFile(path!, 'utf8')
    expect(csv).toContain('额度（整数单位）')
    expect(csv).toContain(String(Number.MAX_SAFE_INTEGER))
    expect(csv).toContain("'=untrusted")
    await expect(dialog).not.toBeVisible()
    await expect(
      page.getByText('已导出 1 条记录', { exact: true })
    ).toBeVisible()
    if (process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS === 'true') {
      await page.screenshot({ path: testInfo.outputPath('export-success.png') })
    }
    fail = true
    await page.getByRole('button', { name: '导出', exact: true }).click()
    await dialog.getByRole('button', { name: '确认', exact: true }).click()
    await expect(
      page.getByText('Export unavailable', { exact: true })
    ).toBeVisible()
    // Without dates the export covers all logs: both bounds are open.
    expect(new URL(exportURL).searchParams.get('start_timestamp')).toBe('0')
    expect(new URL(exportURL).searchParams.get('end_timestamp')).toBe('0')
    await expect(dialog).toBeVisible()
    await expectUnclipped(dialog.getByRole('heading', { name: '导出数据' }))
    await expectUnclipped(
      dialog.getByRole('button', { name: '确认', exact: true })
    )
    if (process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS === 'true') {
      await page.screenshot({ path: testInfo.outputPath('export-error.png') })
    }
    // A full server rejects the export at once; the dialog stays open.
    busyServer = true
    let downloaded = false
    page.on('download', () => (downloaded = true))
    await dialog.getByRole('button', { name: '确认', exact: true }).click()
    await expect(
      page.getByText('导出任务繁忙，请稍后再试。', { exact: true })
    ).toBeInViewport({ ratio: 1 })
    await expect(dialog).toBeVisible()
    expect(downloaded).toBe(false)
    if (process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS === 'true') {
      await page.screenshot({ path: testInfo.outputPath('export-busy.png') })
    }
    // A running export shows its count and turns confirm into cancel.
    await page.evaluate(() => {
      ;(window as { holdExport?: boolean }).holdExport = true
    })
    await dialog.getByRole('button', { name: '确认', exact: true }).click()
    await expect(
      dialog.getByRole('status').getByText('已导出 3 条', { exact: true })
    ).toBeVisible()
    const cancel = dialog.getByRole('button', { name: '取消导出', exact: true })
    await expectUnclipped(cancel)
    await expect(
      dialog.getByRole('button', { name: '确认', exact: true })
    ).toHaveCount(0)
    await assertNoHorizontalOverflow(page)
    if (process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS === 'true') {
      await page.screenshot({
        path: testInfo.outputPath('export-progress.png'),
      })
    }
    await cancel.click()
    await expect(
      dialog.getByRole('button', { name: '确认', exact: true })
    ).toBeVisible()
    await expect(dialog.getByRole('status')).toHaveCount(0)
    await expect(dialog).toBeVisible()
    await assertNoHorizontalOverflow(page)
  })
}
