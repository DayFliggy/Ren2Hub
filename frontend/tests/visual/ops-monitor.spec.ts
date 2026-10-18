import { expect, test, type Page } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const task = {
  task_id: 'cleanup-native',
  type: 'log_cleanup',
  status: 'running',
  created_at: 1785114000,
  updated_at: 1785114100,
  locked_by: 'single-node',
  error: '',
  state: { progress: 25, processed: 25, total: 100 },
  result: {},
}
async function nativeTasks(page: Page, empty = false) {
  const requests: URL[] = []
  let failActive = false
  await page.route('**/api/system-task/list?*', (route) => {
    const url = new URL(route.request().url())
    requests.push(url)
    const active = url.searchParams.get('scope') === 'active'
    if (active && failActive)
      return route.fulfill({
        status: 503,
        json: { success: false, message: 'Task refresh unavailable' },
      })
    const rows = empty
      ? []
      : active
        ? [task]
        : [
            {
              ...task,
              task_id: 'history-native',
              status: 'failed',
              error: '<upstream failure>',
              state: {},
              result: {},
            },
          ]
    return route.fulfill({
      json: { success: true, data: rows, total: rows.length },
    })
  })
  return {
    requests,
    fail: () => {
      failActive = true
    },
    recover: () => {
      failActive = false
    },
  }
}

for (const theme of ['light', 'dark'] as const)
  for (const width of [1440, 390]) {
    test(`native runtime and task history ${theme} ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 })
      await configureStablePage(page, { theme })
      const fixture = await nativeTasks(page)
      await page.goto('/ops')
      await expect(
        page.getByText('Go 运行时内存', { exact: true })
      ).toBeVisible()
      const active = page.getByRole('region', { name: '活动任务', exact: true })
      await expect(active.getByText('运行中', { exact: true })).toBeVisible()
      await expect(active.getByRole('progressbar')).toHaveAttribute(
        'value',
        '25'
      )
      await active
        .getByRole('button', { name: '任务详情', exact: true })
        .click()
      const dialog = page.getByRole('dialog', { name: '任务详情', exact: true })
      await expect(dialog).toContainText('cleanup-native')
      await expect(dialog).toContainText('25 / 100')
      await page.keyboard.press('Escape')
      await expect(dialog).toHaveCount(0)
      const history = page.getByRole('region', {
        name: '任务历史',
        exact: true,
      })
      await history.getByRole('button', { name: '展开', exact: true }).click()
      await expect(history).toContainText('<upstream failure>')
      await expect(history.locator('script')).toHaveCount(0)
      await history.getByLabel('筛选任务类型').fill('log_cleanup')
      await history.getByLabel('筛选任务状态').selectOption('failed')
      await expect
        .poll(() => fixture.requests.at(-1)?.searchParams.get('status'))
        .toBe('failed')
      expect(fixture.requests.at(-1)?.searchParams.get('type')).toBe(
        'log_cleanup'
      )
      expect(fixture.requests.at(-1)?.searchParams.get('offset')).toBe('0')
      await assertNoHorizontalOverflow(page)
    })
  }

test('native task refresh failures remain visible with previous data and recover independently', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'dark' })
  const fixture = await nativeTasks(page)
  await page.goto('/ops')
  const active = page.getByRole('region', { name: '活动任务', exact: true })
  await expect(active).toContainText('运行中')
  fixture.fail()
  await active.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(active.getByRole('alert')).toContainText(
    'Task refresh unavailable'
  )
  await expect(active).toContainText('运行中')
  await expect(page.getByText('Go 运行时内存', { exact: true })).toBeVisible()
  fixture.recover()
  await active.getByRole('button', { name: '重试', exact: true }).click()
  await expect(active.getByRole('alert')).toHaveCount(0)
})

test('native empty task lists remain empty and performance reads recover after failure', async ({
  page,
}) => {
  await configureStablePage(page, { theme: 'light' })
  await nativeTasks(page, true)
  let failed = true
  await page.route('**/api/performance/stats', (route) =>
    failed
      ? route.fulfill({
          status: 503,
          json: { success: false, message: 'Runtime unavailable' },
        })
      : route.fallback()
  )
  await page.goto('/ops')
  await expect(page.getByRole('alert')).toContainText('Runtime unavailable')
  await expect(
    page.getByText('暂无等待或运行中的任务', { exact: true })
  ).toBeVisible()
  const history = page.getByRole('region', { name: '任务历史', exact: true })
  await history.getByRole('button', { name: '展开', exact: true }).click()
  await expect(history).toContainText('暂无已完成的任务')
  failed = false
  await page.getByRole('button', { name: '刷新', exact: true }).first().click()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.getByText('Go 运行时内存', { exact: true })).toBeVisible()
})

for (const role of [1, 10])
  test(`non-root role ${role} cannot request native operations`, async ({
    page,
  }) => {
    await configureStablePage(page, { theme: 'light', user: { role } })
    const operations: string[] = []
    page.on('request', (request) => {
      const path = new URL(request.url()).pathname
      if (
        path.startsWith('/api/performance') ||
        path.startsWith('/api/system-task')
      )
        operations.push(path)
    })
    await page.goto('/ops')
    await expect(page).not.toHaveURL(/\/ops$/)
    await expect(
      page.getByRole('link', { name: '运维监控', exact: true })
    ).toHaveCount(0)
    expect(operations).toEqual([])
  })
