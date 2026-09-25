import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { beforeAll, afterEach, describe, expect, it, vi } from 'vitest'
import TaskPluginsView from '../TaskPluginsView.vue'
import { httpTransport } from '@/api/httpTransport'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

let wrapper: VueWrapper | undefined
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
})

async function fixture(replies: Array<Record<string, unknown>>) {
  const posted: Array<{ url: string; data: unknown }> = []
  let enabled = true
  const request = vi
    .spyOn(httpTransport, 'request')
    .mockImplementation(async (method, url, options) => {
      if (method === 'POST') {
        posted.push({ url, data: options?.data })
        const response = replies.shift()
        if (!response) throw new Error('Unexpected mutation')
        if (response.success) enabled = false
        return response as never
      }
      if (url === '/api/plugin/task')
        return {
          success: true,
          data: [
            {
              meta: {
                key: 'fixture',
                name: 'Fixture',
                version: '1',
                apiVersion: 1,
                models: ['task-model'],
              },
              source: 'custom',
              source_hash: 'hash',
              active: true,
              enabled,
              runtime_status: 'registered',
              channel_count: 2,
              in_flight_count: 0,
            },
          ],
        } as never
      if (url.endsWith('/runtime/status'))
        return {
          success: true,
          data: {
            current_generation: 1,
            last_rebuild: { status: 'success' },
            plugin_errors: {},
          },
        } as never
      if (url.endsWith('/marketplace/sources'))
        return { success: true, data: [] } as never
      throw new Error(`Unexpected ${method} ${url}`)
    })
  wrapper = mount(TaskPluginsView, {
    global: {
      plugins: [i18n],
      stubs: { Teleport: true, ModelPricingEditor: true },
    },
  })
  await flushPromises()
  return { posted, request }
}
function button(text: string, scope: VueWrapper = wrapper!) {
  const found = scope.findAll('button').find((item) => item.text() === text)
  expect(found, `button ${text}`).toBeDefined()
  return found!
}
const channels = [
  { id: 7, name: 'Dedicated channel', type: 61 },
  { id: 8, name: 'Shared gateway', type: 60 },
]
function inUse(count = 0) {
  return {
    success: false,
    message: 'task plugin is still in use',
    data: { channels, in_flight_count: count },
  }
}

describe('plugin disable confirmation and server usage contract', () => {
  it('does not replay a pending disable or close its confirmation', async () => {
    const { request } = await fixture([])
    let resolve!: (value: never) => void
    const pending = new Promise<never>((done) => {
      resolve = done
    })
    await button('Disable').trigger('click')
    request.mockReturnValue(pending)
    await button('Confirm disable').trigger('click')
    const count = request.mock.calls.length
    await button('Confirm disable').trigger('click')
    await button('Cancel').trigger('click')
    expect(request.mock.calls).toHaveLength(count)
    expect(wrapper!.find('[role=dialog]').exists()).toBe(true)
    resolve({ success: false, message: 'Retry later' } as never)
    await flushPromises()
    expect(wrapper!.get('[role=dialog]').text()).toContain('Retry later')
    await button('Cancel').trigger('click')
    expect(wrapper!.find('[role=dialog]').exists()).toBe(false)
  })
  it('cancels without writing, then disables an unbound plugin only after confirmation', async () => {
    const { posted } = await fixture([{ success: true, data: null }])
    await button('Disable').trigger('click')
    expect(posted).toHaveLength(0)
    await button('Cancel').trigger('click')
    expect(posted).toHaveLength(0)
    await button('Disable').trigger('click')
    await button('Confirm disable').trigger('click')
    await flushPromises()
    expect(posted).toEqual([
      { url: '/api/plugin/task/fixture/status', data: { enabled: false } },
    ])
    expect(wrapper!.find('[role=dialog]').exists()).toBe(false)
    expect(button('Enable').exists()).toBe(true)
  })
  it('shows each channel impact, cancels usage confirmation and sends cascade only on an explicit retry', async () => {
    const { posted } = await fixture([
      inUse(),
      inUse(),
      { success: true, data: { disabled_channels: 1, unbound_channels: 1 } },
    ])
    await button('Disable').trigger('click')
    await button('Confirm disable').trigger('click')
    await flushPromises()
    expect(wrapper!.text()).toContain('Dedicated channel')
    expect(wrapper!.text()).toContain('Shared gateway')
    expect(wrapper!.text()).toContain(
      'Other plugins and ordinary traffic remain available'
    )
    expect(posted).toHaveLength(1)
    await button('Cancel').trigger('click')
    expect(posted).toHaveLength(1)
    await button('Disable').trigger('click')
    await button('Confirm disable').trigger('click')
    await flushPromises()
    await button('Cascade disable channels').trigger('click')
    await flushPromises()
    expect(posted[2]).toEqual({
      url: '/api/plugin/task/fixture/status?cascade=true',
      data: { enabled: false },
    })
    expect(posted.some((call) => call.url.includes('force'))).toBe(false)
    expect(wrapper!.find('[role=dialog]').exists()).toBe(false)
  })
  it('refreshes usage after a new in-flight task and requires an explicit force action', async () => {
    const { posted } = await fixture([
      inUse(),
      inUse(3),
      { success: true, data: null },
    ])
    await button('Disable').trigger('click')
    await button('Confirm disable').trigger('click')
    await flushPromises()
    await button('Cascade disable channels').trigger('click')
    await flushPromises()
    expect(wrapper!.text()).toContain('3 pending tasks')
    expect(
      button('Cascade disable channels').attributes('disabled')
    ).toBeDefined()
    expect(posted).toHaveLength(2)
    await button('Force disable').trigger('click')
    await flushPromises()
    expect(posted[2]).toEqual({
      url: '/api/plugin/task/fixture/status?cascade=true&force=true',
      data: { enabled: false },
    })
    expect(posted.every((call) => !call.url.includes('/versions/'))).toBe(true)
  })
  it('keeps ordinary server errors visible and never treats malformed usage as permission to force', async () => {
    const { posted } = await fixture([
      {
        success: false,
        message: 'Database unavailable',
        data: { channels: 'invalid', in_flight_count: 2 },
      },
    ])
    await button('Disable').trigger('click')
    await button('Confirm disable').trigger('click')
    await flushPromises()
    expect(wrapper!.get('[role=dialog]').text()).toContain(
      'Database unavailable'
    )
    expect(
      wrapper!.findAll('button').some((b) => b.text() === 'Force disable')
    ).toBe(false)
    expect(posted).toHaveLength(1)
  })
})
