import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/api/types'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import OpsUpdateCard from '../OpsUpdateCard.vue'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  status: vi.fn(),
}))
vi.mock('@/api/client', () => ({ api: { get: mocks.get, post: mocks.post } }))
vi.mock('@/api/public', () => ({ publicApi: { status: mocks.status } }))

const RELEASE_URL = 'https://github.com/DayFliggy/Ren2Hub/releases/tag/v1.0.2'
const check = (overrides: Record<string, unknown> = {}) => ({
  current_version: 'v1.0.1',
  latest_version: 'v1.0.2',
  has_update: true,
  build_type: 'release',
  cached: false,
  release_info: {
    name: 'Ren2Hub v1.0.2',
    body: '## Fixes\n\n- Faster **routing**\n- [Docs](https://example.com/docs)\n- [bad](javascript:alert(1))\n\n<script>alert(1)</script>',
    published_at: '2026-11-20T08:00:00Z',
    html_url: RELEASE_URL,
  },
  ...overrides,
})
const upToDate = () =>
  check({ latest_version: 'v1.0.1', has_update: false, release_info: null })

type Reply = unknown | ((query: unknown, options: unknown) => unknown)
let replies: Record<string, Reply>
function route(path: string, query: unknown, options: unknown) {
  if (!(path in replies)) throw new Error(`Unexpected API ${path}`)
  const reply = replies[path]
  return typeof reply === 'function' ? reply(query, options) : reply
}
const httpError = (status: number) =>
  new ApiError('server says something in English', { status })

let wrapper: VueWrapper | undefined
let reload: ReturnType<typeof vi.fn>
const nativeLocation = window.location

async function setup() {
  wrapper = mount(OpsUpdateCard, {
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          messages: { en: { common: { confirm: 'OK', cancel: 'Cancel' } } },
        }),
      ],
      stubs: {
        ConsoleModal: {
          props: ['open'],
          template:
            '<div v-if="open" data-modal><slot /><slot name="footer" /></div>',
        },
      },
    },
  })
  await flushPromises()
  return wrapper
}
const action = (name: string) => wrapper!.find(`[data-update-action="${name}"]`)
const openDialog = () =>
  wrapper!.findAllComponents(ConfirmDialog).find((d) => d.props('open'))!
async function confirm() {
  openDialog().vm.$emit('confirm')
  await flushPromises()
}
const errorText = () => wrapper!.get('[data-update-error]').text()

beforeEach(() => {
  vi.clearAllMocks()
  reload = vi.fn()
  Object.defineProperty(window, 'location', {
    value: { reload },
    configurable: true,
    writable: true,
  })
  replies = {
    '/api/admin/system/version': { version: 'v1.0.1', build_type: 'release' },
    '/api/admin/system/check-updates': check(),
    '/api/admin/system/rollback-versions': {
      versions: [
        {
          version: '1.0.0',
          published_at: '2026-11-01T00:00:00Z',
          html_url: 'https://github.com/DayFliggy/Ren2Hub/releases/tag/v1.0.0',
        },
        {
          version: '0.9.9',
          published_at: '2026-10-01T00:00:00Z',
          html_url: 'https://github.com/DayFliggy/Ren2Hub/releases/tag/v0.9.9',
        },
      ],
    },
  }
  mocks.get.mockImplementation(async (...args: [string, unknown, unknown]) =>
    route(...args)
  )
  mocks.post.mockImplementation(async (...args: [string, unknown, unknown]) =>
    route(...args)
  )
  mocks.status.mockResolvedValue({ version: 'v1.0.1', start_time: 100 })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
  Object.defineProperty(window, 'location', {
    value: nativeLocation,
    configurable: true,
    writable: true,
  })
})

describe('ops update card', () => {
  it('shows only the version and the unsupported note for a source build', async () => {
    replies['/api/admin/system/version'] = {
      version: 'v1.0.1',
      build_type: 'source',
    }
    const view = await setup()
    expect(view.get('[data-update-current]').text()).toContain('v1.0.1')
    expect(view.text()).toContain(
      'Source builds do not support in-app updates.'
    )
    expect(view.findAll('[data-update-action]')).toHaveLength(0)
    expect(view.find('[data-update-latest]').exists()).toBe(false)
    expect(mocks.get).toHaveBeenCalledTimes(1)
  })

  it('shows an up-to-date release without the update action', async () => {
    replies['/api/admin/system/check-updates'] = upToDate()
    const view = await setup()
    expect(mocks.get).toHaveBeenCalledWith(
      '/api/admin/system/check-updates',
      { force: false },
      expect.anything()
    )
    expect(view.get('[data-update-latest]').text()).toContain('v1.0.1')
    expect(view.text()).toContain('Up to date')
    expect(action('update').exists()).toBe(false)
    for (const name of ['check', 'rollback', 'restart'])
      expect(action(name).exists(), name).toBe(true)
    expect(view.find('[data-update-notes]').exists()).toBe(false)
  })

  it('shows the newer release with notes, date and link', async () => {
    const view = await setup()
    expect(view.get('[data-update-latest]').text()).toContain('v1.0.2')
    expect(view.text()).toContain('Update available')
    expect(view.get('[data-update-published]').text()).toMatch(/2026/)
    const notes = view.get('[data-update-notes]')
    expect(notes.find('h3, h4').text()).toBe('Fixes')
    expect(notes.find('strong').text()).toBe('routing')
    const links = notes.findAll('a')
    expect(links.map((link) => link.attributes('href'))).toEqual([
      'https://example.com/docs',
      undefined,
    ])
    expect(notes.find('script').exists()).toBe(false)
    expect(view.get('[data-update-link]').attributes()).toMatchObject({
      href: RELEASE_URL,
      target: '_blank',
      rel: 'noopener noreferrer',
    })
    expect(action('update').text()).toContain('v1.0.2')
  })

  it('forces a check and disables every action while it runs', async () => {
    const view = await setup()
    let finish: (value: unknown) => void = () => {}
    replies['/api/admin/system/check-updates'] = () =>
      new Promise((resolve) => (finish = resolve))
    await action('check').trigger('click')
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/admin/system/check-updates',
      { force: true },
      expect.anything()
    )
    for (const button of view.findAll('[data-update-action]'))
      expect(button.attributes('disabled'), button.text()).toBeDefined()
    finish(upToDate())
    await flushPromises()
    expect(view.text()).toContain('Up to date')
    for (const button of view.findAll('[data-update-action]'))
      expect(button.attributes('disabled'), button.text()).toBeUndefined()
  })

  it('shows the cached result with a translated warning', async () => {
    replies['/api/admin/system/check-updates'] = check({
      cached: true,
      warning: 'GitHub request failed: dial tcp timeout',
    })
    const view = await setup()
    expect(view.get('[data-update-warning]').text()).toBe(
      'GitHub could not be reached; showing the last cached result.'
    )
    expect(view.text()).not.toContain('dial tcp')
    expect(view.get('[data-update-latest]').text()).toContain('v1.0.2')
  })

  it('maps a failed check to the GitHub message without the server text', async () => {
    replies['/api/admin/system/check-updates'] = () => {
      throw httpError(502)
    }
    const view = await setup()
    expect(errorText()).toContain(
      'Could not reach GitHub for release information.'
    )
    expect(view.text()).not.toContain('server says')
    expect(view.get('[data-update-current]').text()).toContain('v1.0.1')
    expect(action('update').exists()).toBe(false)
    expect(action('check').exists()).toBe(true)
  })

  it('asks to restart after a confirmed update', async () => {
    replies['/api/admin/system/update'] = { need_restart: true }
    const view = await setup()
    await action('update').trigger('click')
    expect(mocks.post).not.toHaveBeenCalled()
    await confirm()
    expect(mocks.post).toHaveBeenCalledWith(
      '/api/admin/system/update',
      undefined,
      expect.objectContaining({ timeoutMs: expect.any(Number) })
    )
    expect(view.get('[data-update-restart-needed]').text()).toContain(
      'Updated to v1.0.2. Restart to apply it.'
    )
    expect(action('update').exists()).toBe(false)
    expect(action('rollback').exists()).toBe(false)
    expect(action('restart').exists()).toBe(true)
  })

  it('reports an already up-to-date server', async () => {
    replies['/api/admin/system/update'] = {
      already_up_to_date: true,
      current_version: 'v1.0.2',
      latest_version: 'v1.0.2',
    }
    const view = await setup()
    await action('update').trigger('click')
    replies['/api/admin/system/check-updates'] = upToDate()
    await confirm()
    expect(view.get('[data-update-result]').text()).toContain(
      'already on the latest version v1.0.2'
    )
    expect(view.find('[data-update-restart-needed]').exists()).toBe(false)
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/admin/system/check-updates',
      { force: true },
      expect.anything()
    )
    expect(action('update').exists()).toBe(false)
  })

  it.each([
    [409, 'Another update, rollback or restart is in progress.'],
    [502, 'Downloading or verifying the release package failed.'],
    [500, 'the original program was restored'],
    [504, 'The operation may still be running'],
    [undefined, 'The operation may still be running'],
    [418, 'The request failed (HTTP 418).'],
  ])('maps update failures by HTTP status %s', async (status, text) => {
    replies['/api/admin/system/update'] = () => {
      throw status
        ? httpError(status)
        : new ApiError('timeout of 960000ms exceeded')
    }
    const view = await setup()
    await action('update').trigger('click')
    await confirm()
    expect(errorText()).toContain(text)
    expect(view.text()).not.toContain('server says')
    expect(view.find('[data-update-restart-needed]').exists()).toBe(false)
    expect(action('update').attributes('disabled')).toBeUndefined()
  })

  it('rolls back to the selected version and asks to restart', async () => {
    replies['/api/admin/system/rollback'] = {
      need_restart: true,
      version: '1.0.0',
    }
    const view = await setup()
    await action('rollback').trigger('click')
    await flushPromises()
    const select = view.getComponent(FilterSelect)
    expect(
      select.props('options').map((o: { label: string }) => o.label)
    ).toEqual([
      expect.stringContaining('v1.0.0'),
      expect.stringContaining('v0.9.9'),
    ])
    const submit = view.get('[data-update-rollback-confirm]')
    expect(submit.attributes('disabled')).toBeDefined()
    select.vm.$emit('update:modelValue', '1.0.0')
    await flushPromises()
    await submit.trigger('click')
    await flushPromises()
    expect(mocks.post).toHaveBeenCalledWith(
      '/api/admin/system/rollback',
      { version: '1.0.0' },
      expect.objectContaining({ timeoutMs: expect.any(Number) })
    )
    expect(
      view.find('[data-modal] [data-update-rollback-confirm]').exists()
    ).toBe(false)
    expect(view.get('[data-update-restart-needed]').text()).toContain(
      'Rolled back to v1.0.0. Restart to apply it.'
    )
  })

  it('shows an empty rollback list', async () => {
    replies['/api/admin/system/rollback-versions'] = { versions: [] }
    const view = await setup()
    await action('rollback').trigger('click')
    await flushPromises()
    expect(view.get('[data-modal]').text()).toContain(
      'No earlier release is available.'
    )
    expect(
      view.find('[data-update-rollback-confirm]').attributes('disabled')
    ).toBeDefined()
  })

  it('maps a rollback that is not in the list', async () => {
    replies['/api/admin/system/rollback'] = () => {
      throw httpError(404)
    }
    const view = await setup()
    await action('rollback').trigger('click')
    await flushPromises()
    view.getComponent(FilterSelect).vm.$emit('update:modelValue', '0.9.9')
    await flushPromises()
    await view.get('[data-update-rollback-confirm]').trigger('click')
    await flushPromises()
    expect(view.get('[data-modal] [data-update-error]').text()).toContain(
      'This version is not in the rollback list.'
    )
    expect(view.find('[data-update-restart-needed]').exists()).toBe(false)
  })

  it('reloads after the restarted server reports the target version', async () => {
    replies['/api/admin/system/update'] = { need_restart: true }
    replies['/api/admin/system/restart'] = { restarting: true }
    const view = await setup()
    await action('update').trigger('click')
    await confirm()
    vi.useFakeTimers()
    mocks.status
      .mockResolvedValueOnce({ version: 'v1.0.1', start_time: 100 })
      .mockRejectedValueOnce(new ApiError('Bad Gateway', { status: 502 }))
      .mockResolvedValue({ version: 'v1.0.2', start_time: 200 })
    await view.get('[data-update-restart-needed] button').trigger('click')
    openDialog().vm.$emit('confirm')
    await vi.advanceTimersByTimeAsync(0)
    expect(mocks.post).toHaveBeenLastCalledWith(
      '/api/admin/system/restart',
      undefined,
      undefined
    )
    expect(view.get('[data-update-restart-state]').text()).toContain(
      'Restarting'
    )
    expect(reload).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(10_000)
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('reports an unknown state when the restart is not observed', async () => {
    replies['/api/admin/system/restart'] = { restarting: true }
    const view = await setup()
    vi.useFakeTimers()
    await action('restart').trigger('click')
    openDialog().vm.$emit('confirm')
    await vi.advanceTimersByTimeAsync(125_000)
    expect(view.get('[data-update-restart-state]').text()).toContain(
      'could not be confirmed within 120 seconds'
    )
    expect(reload).not.toHaveBeenCalled()
    await view.get('[data-update-restart-state] button').trigger('click')
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('keeps the page when a restart is refused', async () => {
    replies['/api/admin/system/restart'] = () => {
      throw httpError(409)
    }
    const view = await setup()
    await action('restart').trigger('click')
    await confirm()
    expect(errorText()).toContain(
      'Another update, rollback or restart is in progress.'
    )
    expect(view.find('[data-update-restart-state]').exists()).toBe(false)
    // Only the baseline read before the POST; no restart polling follows.
    expect(mocks.status).toHaveBeenCalledTimes(1)
  })

  it('aborts reads on unmount and ignores late results', async () => {
    const signals: AbortSignal[] = []
    let finish: (value: unknown) => void = () => {}
    replies['/api/admin/system/check-updates'] = (
      _query: unknown,
      options: { signal: AbortSignal }
    ) => {
      signals.push(options.signal)
      return new Promise((resolve) => (finish = resolve))
    }
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const view = await setup()
    expect(signals).toHaveLength(1)
    view.unmount()
    wrapper = undefined
    expect(signals[0]!.aborted).toBe(true)
    finish(check())
    await flushPromises()
    // The late answer neither starts follow-up reads nor touches the unmounted card.
    expect(mocks.get).toHaveBeenCalledTimes(2)
    expect(warn).not.toHaveBeenCalled()
    warn.mockRestore()
  })
})
