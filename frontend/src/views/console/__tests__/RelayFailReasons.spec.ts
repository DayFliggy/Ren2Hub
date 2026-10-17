import { createPinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

import { api } from '@/api/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { mergeServerMessages } from '@/i18n/serverMessages'
import DrawingLogsView from '@/views/console/DrawingLogsView.vue'
import TaskLogsView from '@/views/console/TaskLogsView.vue'

const PROVIDER_REASON = 'upstream said {{quota}} @limit | retry'

let wrapper: VueWrapper | null = null

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
  mergeServerMessages('zh-CN', { 'Task timed out': '任务超时' })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

function page(items: unknown[]) {
  return Promise.resolve({ page: 1, page_size: 10, total: items.length, items })
}

function mountView(component: typeof TaskLogsView) {
  return mount(component, {
    attachTo: document.body,
    global: {
      plugins: [createPinia(), i18n],
      stubs: { LogsNavTabs: true, TaskArtifactsCell: true },
    },
  })
}

describe('relay task fail reasons', () => {
  it('translates gateway fail reasons and keeps provider text', async () => {
    const task = {
      platform: 'suno',
      action: 'MUSIC',
      status: 'FAILURE',
      progress: '100%',
      quota: '0',
      submit_time: 1_700_000_000,
      finish_time: 1_700_000_100,
    }
    vi.spyOn(api, 'get').mockImplementation(() =>
      page([
        { ...task, id: 1, task_id: 'task-1', fail_reason: 'Task timed out' },
        { ...task, id: 2, task_id: 'task-2', fail_reason: PROVIDER_REASON },
      ])
    )
    wrapper = mountView(TaskLogsView)
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('任务超时')
    expect(text).not.toContain('Task timed out')
    expect(text).toContain(PROVIDER_REASON)
  })

  it('translates a gateway fail reason in the drawing task details', async () => {
    vi.spyOn(api, 'get').mockImplementation(() =>
      page([
        {
          id: 3,
          mj_id: 'mj-3',
          action: 'IMAGINE',
          prompt: 'a lighthouse',
          prompt_en: 'a lighthouse',
          status: 'FAILURE',
          progress: '100%',
          fail_reason: 'Task timed out',
          image_url: '',
          video_url: '',
          quota: '0',
          submit_time: 1_700_000_000_000,
          finish_time: 1_700_000_100_000,
        },
      ])
    )
    wrapper = mountView(DrawingLogsView)
    await flushPromises()

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'a lighthouse')!
      .trigger('click')
    await flushPromises()

    expect(document.body.textContent).toContain('任务超时')
    expect(document.body.textContent).not.toContain('Task timed out')
  })
})
