import { createPinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'

import { api } from '@/api/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { mergeServerMessages } from '@/i18n/serverMessages'
import LogsView from '@/views/console/LogsView.vue'

let wrapper: VueWrapper | null = null

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

beforeEach(() => {
  vi.spyOn(api, 'get').mockImplementation((path: string) => {
    if (path === '/api/user/self/groups')
      return Promise.resolve({ default: { ratio: 1, desc: 'Default' } })
    if (path === '/api/log/self') {
      return Promise.resolve({ page: 1, page_size: 10, total: 0, items: [] })
    }
    if (path === '/api/log/self/stat') {
      return Promise.resolve({
        total_requests: 0,
        total_quota: 0,
        today_requests: 0,
        today_quota: 0,
      })
    }
    return Promise.reject(new Error(`Unexpected API request: ${path}`))
  })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
})

describe('LogsView business log filters', () => {
  it('scopes the call logs tab to call types (2,5) and excludes non-call/account types', async () => {
    wrapper = mount(LogsView, {
      attachTo: document.body,
      global: {
        plugins: [createPinia(), i18n],
        stubs: { LogsNavTabs: true },
      },
    })
    await flushPromises()

    const calls = vi
      .mocked(api.get)
      .mock.calls.filter((call) => call[0] === '/api/log/self')
    expect(calls[0]?.[1]).toMatchObject({ type: 0, types: '2,5' })

    await wrapper
      .findAll('[data-control-container]')
      .find((control) => control.find('span[hidden]').text() === '类型')!
      .get('[role="combobox"]')
      .trigger('click')
    const options = [...document.body.querySelectorAll('[role="option"]')].map(
      (item) => item.textContent?.trim()
    )

    expect(options).toEqual(['全部调用', '正常调用', '调用失败'])
    expect(options).not.toContain('充值')
    expect(options).not.toContain('退款')
    expect(options).not.toContain('管理')
    expect(options).not.toContain('系统')
    expect(options).not.toContain('登录')
  })

  it('forwards model_name and token_name filters cleanly', async () => {
    wrapper = mount(LogsView, {
      global: { plugins: [createPinia(), i18n], stubs: { LogsNavTabs: true } },
    })
    await flushPromises()
    await wrapper.get('input[name="log-model-search"]').setValue('gpt-4%')
    await wrapper.get('input[name="log-token-search"]').setValue('prod-key')
    await new Promise((resolve) => setTimeout(resolve, 350))
    await flushPromises()

    const calls = vi
      .mocked(api.get)
      .mock.calls.filter((call) => call[0] === '/api/log/self')
    expect(calls.at(-1)?.[1]).toMatchObject({
      model_name: 'gpt-4%',
      token_name: 'prod-key',
      types: '2,5',
    })
  })
})

describe('LogsView log content', () => {
  it('shows content_parts and a channel test token in the console language', async () => {
    mergeServerMessages('zh-CN', {
      'Model test': '模型测试',
      'Channel test for {{model}} took {{seconds}}s':
        '渠道测试 {{model}} 用时 {{seconds}} 秒',
    })
    vi.mocked(api.get).mockImplementation((path: string) => {
      if (path === '/api/user/self/groups')
        return Promise.resolve({ default: { ratio: 1, desc: 'Default' } })
      if (path === '/api/log/self/stat') {
        return Promise.resolve({
          total_requests: 1,
          total_quota: 0,
          today_requests: 1,
          today_quota: 0,
        })
      }
      return Promise.resolve({
        page: 1,
        page_size: 10,
        total: 1,
        items: [
          {
            id: 21,
            type: 2,
            token_id: 0,
            token_name: 'Model test',
            model_name: 'gpt-4o',
            channel_name: 'OpenAI',
            prompt_tokens: 0,
            completion_tokens: 0,
            other: JSON.stringify({
              content_parts: [
                {
                  key: 'Channel test for {{model}} took {{seconds}}s',
                  params: { model: 'gpt-4o', seconds: 1.5 },
                },
              ],
            }),
            quota: 0,
            use_time: 1500,
            is_stream: false,
            content: 'Channel test for gpt-4o took 1.5s',
            created_at: 1_700_000_000,
          },
        ],
      })
    })
    wrapper = mount(LogsView, {
      attachTo: document.body,
      global: {
        plugins: [createPinia(), i18n],
        stubs: { LogsNavTabs: true },
      },
    })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('渠道测试 gpt-4o 用时 1.5 秒')
    expect(text).toContain('模型测试')
    expect(text).not.toContain('Channel test for gpt-4o')
    expect(text).not.toContain('Model test')
  })
})

it('clears previously displayed rows and shows an error when filtering fails', async () => {
  let fail = false
  vi.mocked(api.get).mockImplementation((path: string) => {
    if (path === '/api/user/self/groups')
      return Promise.resolve({ default: { ratio: 1, desc: 'Default' } })
    if (path === '/api/log/self/stat')
      return Promise.resolve({
        total_requests: 1,
        total_quota: 1,
        today_requests: 1,
        today_quota: 1,
      })
    if (path === '/api/log/self') {
      if (fail) return Promise.reject(new Error('Filter query failed'))
      return Promise.resolve({
        page: 1,
        page_size: 10,
        total: 1,
        items: [
          {
            id: 77,
            type: 2,
            token_id: 0,
            token_name: 'fixture-token',
            model_name: 'fixture-model',
            channel_name: 'fixture-channel',
            prompt_tokens: 1,
            completion_tokens: 1,
            quota: 1,
            use_time: 1,
            is_stream: false,
            content: 'ok',
            created_at: 1_700_000_000,
          },
        ],
      })
    }
    return Promise.reject(new Error(`Unexpected API request: ${path}`))
  })
  wrapper = mount(LogsView, {
    attachTo: document.body,
    global: { plugins: [createPinia(), i18n], stubs: { LogsNavTabs: true } },
  })
  await flushPromises()
  expect(wrapper.find('[data-log-model]').exists()).toBe(true)

  fail = true
  await wrapper.get('input[name="log-search"]').setValue('bad-range')
  await new Promise((resolve) => setTimeout(resolve, 350))
  await flushPromises()

  expect(wrapper.find('[data-log-model]').exists()).toBe(false)
  expect(wrapper.text()).toContain('日志加载失败')
  expect(wrapper.text()).toContain('失败')
})

it('filters native logs by request_id without sending the retired keyword parameter', async () => {
  wrapper = mount(LogsView, {
    global: { plugins: [createPinia(), i18n], stubs: { LogsNavTabs: true } },
  })
  await flushPromises()
  await wrapper.get('input[name="log-search"]').setValue('native-request')
  await new Promise((resolve) => setTimeout(resolve, 350))
  await flushPromises()
  const calls = vi
    .mocked(api.get)
    .mock.calls.filter((call) => call[0] === '/api/log/self')
  expect(calls.at(-1)?.[1]).toMatchObject({ request_id: 'native-request' })
  expect(calls.at(-1)?.[1]).not.toHaveProperty('keyword')
})
