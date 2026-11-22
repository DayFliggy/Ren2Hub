import { readFile } from 'node:fs/promises'
import { expect, test, type Locator, type Page } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

// Response of a real export rejected while the same user already runs one.
const runningFixture = new URL(
  '../fixtures/native/log-export-running.json',
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
/**
 * Route fulfilment sends a whole body at once. To show a running export,
 * fetch answers it with `count` rows and keeps the stream open until the
 * request is aborted, once the page sets `window.holdExport`.
 */
async function holdExportStreams(page: Page, count: number) {
  await page.addInitScript(
    ({ line, count }) => {
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
              new TextEncoder().encode(`${line}\n`.repeat(count))
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
    },
    { line: JSON.stringify(row), count }
  )
}

/** Serves the dev module with small files so a few rows span several. */
async function useSmallExportVolumes(page: Page, bytes: number) {
  await page.route('**/src/utils/exportVolumes.ts*', async (route) => {
    const response = await route.fetch()
    const source = await response.text()
    const patched = source.replace(
      /EXPORT_VOLUME_BYTES = [^;\n]+/,
      `EXPORT_VOLUME_BYTES = ${bytes}`
    )
    if (patched === source) throw new Error('volume size constant not found')
    await route.fulfill({ response, body: patched })
  })
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
    await holdExportStreams(page, 3)
    let exportURL = ''
    let fail = false
    let runningServer = false
    // The fixture file is pretty-printed; the server writes compact JSON.
    const running = JSON.stringify(
      JSON.parse(await readFile(runningFixture, 'utf8'))
    )
    await page.route('**/api/log/self/export?*', (route) => {
      exportURL = route.request().url()
      if (runningServer)
        return route.fulfill({
          contentType: 'application/json',
          body: running,
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
    // Another export of the same user is rejected at once; the dialog stays open.
    runningServer = true
    let downloaded = false
    page.on('download', () => (downloaded = true))
    await dialog.getByRole('button', { name: '确认', exact: true }).click()
    await expect(
      page.getByText('已有日志导出正在运行，请等待完成后重试。', {
        exact: true,
      })
    ).toBeInViewport({ ratio: 1 })
    await expect(dialog).toBeVisible()
    expect(downloaded).toBe(false)
    if (process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS === 'true') {
      await page.screenshot({ path: testInfo.outputPath('export-running.png') })
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

  test(`log export volumes ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
    )
    await configureStablePage(page, { theme })
    await useSmallExportVolumes(page, 1024)
    await holdExportStreams(page, 10)
    const rows = 10
    await page.route('**/api/log/self/export?*', (route) =>
      route.fulfill({
        contentType: 'application/x-ndjson',
        body:
          `${JSON.stringify(row)}\n`.repeat(rows) +
          JSON.stringify({ success: true, data: { count: rows } }),
      })
    )
    const downloads: string[] = []
    page.on('download', (download) =>
      downloads.push(download.suggestedFilename())
    )
    await Promise.all([
      page.waitForResponse('**/api/log/self?*'),
      page.goto('/logs'),
    ])
    const notice =
      '导出较大，将分多个文件下载。浏览器若询问是否允许下载多个文件，请选择允许。'
    const dialog = page.getByRole('dialog', { name: '导出数据' })

    // A running export that already saved a file explains the next downloads.
    await page.evaluate(() => {
      ;(window as { holdExport?: boolean }).holdExport = true
    })
    await page.getByRole('button', { name: '导出', exact: true }).click()
    await dialog.getByRole('button', { name: '确认', exact: true }).click()
    await expect(dialog.getByText(notice, { exact: true })).toBeVisible()
    await expectUnclipped(dialog.getByText(notice, { exact: true }))
    await expectUnclipped(
      dialog.getByRole('button', { name: '取消导出', exact: true })
    )
    await assertNoHorizontalOverflow(page)
    if (process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS === 'true') {
      await page.screenshot({
        path: testInfo.outputPath('export-multiple-notice.png'),
      })
    }
    await dialog.getByRole('button', { name: '取消导出', exact: true }).click()
    await expect(dialog.getByText(notice, { exact: true })).toHaveCount(0)
    await expect(
      page.getByText(/^导出已取消，已下载的 \d+ 个文件/)
    ).toBeVisible()

    // A finished multi-file export names the expected file count.
    await page.evaluate(() => {
      ;(window as { holdExport?: boolean }).holdExport = false
    })
    downloads.length = 0
    await dialog.getByRole('button', { name: '确认', exact: true }).click()
    await expect(dialog).not.toBeVisible()
    const message = page.getByText(/^已导出 10 条，分 \d+ 个文件；/)
    await expect(message).toBeInViewport({ ratio: 1 })
    const files = Number(
      (await message.textContent())!.match(/分 (\d+) 个/)![1]
    )
    expect(files).toBeGreaterThan(1)
    await expect.poll(() => downloads.length).toBe(files)
    expect(downloads).toEqual(
      Array.from(
        { length: files },
        (_, index) =>
          expect.stringMatching(
            new RegExp(`^ren2hub-logs-all-\\d{8}-part${index + 1}\\.csv$`)
          ) as unknown as string
      )
    )
    await expect(message).toHaveText(
      `已导出 10 条，分 ${files} 个文件；若下载列表中少于 ${files} 个文件，请允许本站下载多个文件后重新导出。`
    )
    await assertNoHorizontalOverflow(page)
    if (process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS === 'true') {
      await page.screenshot({
        path: testInfo.outputPath('export-multiple-done.png'),
      })
    }
  })
}

test('log export running English narrow', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 320, height: 800 })
  await configureStablePage(page, { theme: 'light', locale: 'en' })
  await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
  const running = JSON.stringify(
    JSON.parse(await readFile(runningFixture, 'utf8'))
  )
  await page.route('**/api/log/self/export?*', (route) =>
    route.fulfill({ contentType: 'application/json', body: running })
  )
  await Promise.all([
    page.waitForResponse('**/api/log/self?*'),
    page.goto('/logs'),
  ])
  await page.getByRole('button', { name: 'Export', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Export data' })
  await expect(dialog).toBeVisible()
  let downloaded = false
  page.on('download', () => (downloaded = true))
  await dialog.getByRole('button', { name: 'Confirm', exact: true }).click()
  await expect(
    page.getByText(
      'Another log export is running. Try again after it finishes.',
      { exact: true }
    )
  ).toBeInViewport({ ratio: 1 })
  await expect(dialog).toBeVisible()
  expect(downloaded).toBe(false)
  await assertNoHorizontalOverflow(page)
  if (process.env.PLAYWRIGHT_CAPTURE_SCREENSHOTS === 'true') {
    await page.screenshot({
      path: testInfo.outputPath('export-running-en.png'),
    })
  }
})
