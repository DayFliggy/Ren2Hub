import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, expect, it, onTestFinished, vi } from 'vitest'
import { ApiError } from '@/api/types'
import { useSystemSettings } from '@/composables/useSystemSettings'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const { api, backend, toast } = vi.hoisted(() => {
  const backend = {
    options: {} as Record<string, string>,
    optionsError: null as Error | null,
  }
  const policyKey = (key: string) =>
    key.startsWith('channel_affinity_setting.') ||
    ['RetryTimes', 'CheckSensitiveEnabled', 'SensitiveWords'].includes(key)
  // GetRequestPolicy answers GET and PATCH with the full stored snapshot.
  const snapshot = () => ({
    options: Object.fromEntries(
      Object.entries(backend.options).filter(([key]) => policyKey(key))
    ),
  })
  const api = {
    get: vi.fn(async (path: string) => {
      if (path === '/api/option/request_policy') return snapshot()
      if (path !== '/api/option/') throw new Error('offline')
      if (backend.optionsError) throw backend.optionsError
      return Object.entries(backend.options).map(([key, value]) => ({
        key,
        value,
      }))
    }),
    patch: vi.fn(
      async (_path: string, body: { options: Record<string, string> }) => {
        Object.assign(backend.options, body.options)
        return snapshot()
      }
    ),
    put: vi.fn(async (_path: string, body: { key: string; value: string }) => {
      backend.options[body.key] = body.value
    }),
  }
  return {
    api,
    backend,
    toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
  }
})
vi.mock('@/api/client', () => ({ api }))
vi.mock('@/composables/useToast', () => ({ useToast: () => toast }))
import RequestPolicyPanel from '../RequestPolicyPanel.vue'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => {
  vi.clearAllMocks()
  Object.assign(backend, {
    options: {
      RetryTimes: '2',
      CheckSensitiveEnabled: 'false',
      SensitiveWords: 'old',
      'channel_affinity_setting.rules': '[]',
      SystemName: 'Ren2Hub',
    },
    optionsError: null,
  })
})
async function render() {
  const wrapper = mount(RequestPolicyPanel, {
    global: { plugins: [i18n] },
  })
  await flushPromises()
  return wrapper
}
function draft(wrapper: Awaited<ReturnType<typeof render>>) {
  return JSON.parse(wrapper.get('textarea').element.value)
}
async function editAndSave(
  wrapper: Awaited<ReturnType<typeof render>>,
  changes: Record<string, unknown>
) {
  const next = JSON.stringify({ ...draft(wrapper), ...changes })
  await wrapper.get('textarea').setValue(next)
  await wrapper.findAll('button')[0]!.trigger('click')
  await flushPromises()
}
function patchedOptions() {
  return api.patch.mock.calls.map(([, body]) => body.options)
}

it('patches only the edited key and shows the merged snapshot', async () => {
  const wrapper = await render()
  await editAndSave(wrapper, { RetryTimes: '4' })
  expect(api.patch).toHaveBeenCalledWith(
    '/api/option/request_policy',
    { options: { RetryTimes: '4' } },
    { signal: expect.any(AbortSignal) }
  )
  expect(draft(wrapper)).toEqual({
    RetryTimes: '4',
    CheckSensitiveEnabled: 'false',
    SensitiveWords: 'old',
    'channel_affinity_setting.rules': '[]',
  })
  wrapper.unmount()
})
it('does not write when the draft has no changed value', async () => {
  const wrapper = await render()
  await editAndSave(wrapper, {})
  expect(api.patch).not.toHaveBeenCalled()
  expect(toast.info).toHaveBeenCalledWith('No changes to save')
  wrapper.unmount()
})
it('refreshes the shared settings after saving', async () => {
  const settings = useSystemSettings()
  expect(await settings.load(true)).toBe(true)
  const wrapper = await render()
  await editAndSave(wrapper, { CheckSensitiveEnabled: 'true' })
  expect(api.get).toHaveBeenLastCalledWith(
    '/api/option/request_policy',
    undefined,
    {
      signal: expect.any(AbortSignal),
    }
  )
  expect(settings.rawOptions.value.CheckSensitiveEnabled).toBe('true')
  expect(toast.error).not.toHaveBeenCalled()
  wrapper.unmount()
})
it('reports a failed settings refresh after the policy was saved', async () => {
  const settings = useSystemSettings()
  const wrapper = await render()
  // The live client rejects with ApiError; load() toasts its message.
  backend.optionsError = new ApiError('Service unavailable', { status: 503 })
  await editAndSave(wrapper, { RetryTimes: '4' })
  expect(backend.options.RetryTimes).toBe('4')
  expect(settings.loaded.value).toBe(false)
  expect(toast.error).toHaveBeenCalledWith('Service unavailable')
  expect(draft(wrapper).RetryTimes).toBe('4')
  expect(wrapper.find('[role=alert]').exists()).toBe(false)
  wrapper.unmount()
})
it.each([
  ['reloads an untouched panel', false],
  ['keeps an edited panel draft', true],
])('keeps a form save made earlier when it %s', async (_, editFirst) => {
  const settings = useSystemSettings()
  expect(await settings.load(true)).toBe(true)
  const wrapper = await render()
  const next = JSON.stringify({ ...draft(wrapper), RetryTimes: '4' })
  if (editFirst) await wrapper.get('textarea').setValue(next)
  expect(await settings.saveOptions({ SensitiveWords: 'new' })).toBe(true)
  await flushPromises()
  expect(draft(wrapper).SensitiveWords).toBe(editFirst ? 'old' : 'new')
  await editAndSave(wrapper, { RetryTimes: '4' })
  expect(patchedOptions()).toEqual([{ RetryTimes: '4' }])
  expect(backend.options).toMatchObject({
    RetryTimes: '4',
    SensitiveWords: 'new',
  })
  expect(draft(wrapper).SensitiveWords).toBe('new')
  wrapper.unmount()
})
it('locks the draft while a reload after a form save is in flight', async () => {
  const settings = useSystemSettings()
  expect(await settings.load(true)).toBe(true)
  const wrapper = await render()
  let release!: () => void
  const gate = new Promise<void>((resolve) => (release = resolve))
  const defaultGet = api.get.getMockImplementation()!
  api.get.mockImplementation(async (path: string) => {
    if (path === '/api/option/request_policy') await gate
    return defaultGet(path)
  })
  onTestFinished(() => {
    release()
    api.get.mockImplementation(defaultGet)
  })
  expect(await settings.saveOptions({ SensitiveWords: 'new' })).toBe(true)
  await flushPromises()
  // Input is blocked until the snapshot lands, so the reload cannot replace
  // text typed after it started.
  expect(wrapper.get('textarea').attributes('disabled')).toBeDefined()
  release()
  await flushPromises()
  expect(wrapper.get('textarea').attributes('disabled')).toBeUndefined()
  expect(draft(wrapper).SensitiveWords).toBe('new')
  wrapper.unmount()
})
it.each(['{"RetryTimes":4}', '[]', 'invalid'])(
  'rejects invalid draft %s without a write',
  async (value) => {
    const wrapper = await render()
    await wrapper.get('textarea').setValue(value)
    await wrapper.findAll('button')[0]!.trigger('click')
    await flushPromises()
    expect(api.patch).not.toHaveBeenCalled()
    expect(wrapper.find('[role=alert]').exists()).toBe(true)
    wrapper.unmount()
  }
)
it('preserves draft and server rejection instead of claiming a successful save', async () => {
  const wrapper = await render()
  api.patch.mockRejectedValueOnce(new Error('RetryTimes is invalid'))
  await editAndSave(wrapper, { RetryTimes: 'wrong' })
  expect(wrapper.get('textarea').element.value).toContain('wrong')
  expect(wrapper.text()).toContain('RetryTimes is invalid')
  expect(api.get).toHaveBeenCalledTimes(1)
  wrapper.unmount()
})
