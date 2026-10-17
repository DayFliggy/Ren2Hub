import { expect, test } from '@playwright/test'
import { assertNoHorizontalOverflow, configureStablePage } from './fixtures'

const access = 'A'.repeat(43)
const imageUrl = `https://gateway.example.test/v1/tasks/task_plugin/artifacts/image-1/content?access=${access}`
// 1x1 transparent PNG so the preview renders instead of the media failure.
const pixel = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=',
  'base64'
)

function taskRow(id: number, taskId: string, extra: Record<string, unknown>) {
  return {
    id,
    task_id: taskId,
    platform: 'alibaba',
    action: 'text_to_image',
    status: 'SUCCESS',
    progress: '100%',
    fail_reason: '',
    result_url: '',
    quota: 25000,
    submit_time: 1785123000 + id,
    finish_time: 1785123060 + id,
    ...extra,
  }
}

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`task log artifacts ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }
      )
      await configureStablePage(page, { theme, authenticated: true })
      const artifactRequests: string[] = []
      await page.route('**/api/task/self?*', (route) =>
        route.fulfill({
          json: {
            success: true,
            data: {
              page: 1,
              page_size: 10,
              total: 3,
              items: [
                taskRow(1, 'task_plugin', {}),
                taskRow(2, 'task_discarded', { result_discarded: true }),
                taskRow(3, 'task_expired', {}),
              ],
            },
          },
        })
      )
      await page.route('**/api/task/*/artifacts', (route) => {
        const taskId = new URL(route.request().url()).pathname.split('/')[3]
        artifactRequests.push(taskId)
        if (taskId === 'task_plugin') {
          return route.fulfill({
            json: {
              success: true,
              data: {
                task_id: taskId,
                artifacts: [
                  {
                    key: 'image-1',
                    type: 'image',
                    mime_type: 'image/png',
                    content_url: imageUrl,
                  },
                ],
              },
            },
          })
        }
        return route.fulfill({
          status: 404,
          json: {
            success: false,
            code: 'artifact_not_found',
            message: 'Task or artifact not found',
          },
        })
      })
      await page.route('https://gateway.example.test/v1/tasks/**', (route) =>
        route.fulfill({ contentType: 'image/png', body: pixel })
      )

      await page.goto('/usage-logs/task')
      await expect(
        page.locator('[data-task-result-discarded]:visible')
      ).toHaveText('结果未保留')
      const triggers = page.locator('[data-task-artifacts-trigger]:visible')
      await expect(triggers).toHaveCount(2)
      expect(artifactRequests).toEqual([])
      await assertNoHorizontalOverflow(page)
      await page.screenshot({ path: testInfo.outputPath('task-list.png') })

      await triggers.first().click()
      const dialog = page.getByRole('dialog', { name: '制品' })
      await expect(dialog.locator('img')).toHaveAttribute('src', imageUrl)
      await expect(
        dialog.getByRole('link', { name: '下载 image-1' })
      ).toHaveAttribute('href', imageUrl)
      await expect(dialog.getByRole('alert')).toHaveCount(0)
      await assertNoHorizontalOverflow(page)
      await page.screenshot({ path: testInfo.outputPath('artifacts.png') })
      await dialog.getByRole('button', { name: '关闭' }).click()

      await triggers.nth(1).click()
      await expect(page.getByRole('dialog', { name: '制品' })).toContainText(
        '暂无制品'
      )
      await page.screenshot({
        path: testInfo.outputPath('artifacts-empty.png'),
      })
      expect(artifactRequests).toEqual(['task_plugin', 'task_expired'])
    })
  }
}
