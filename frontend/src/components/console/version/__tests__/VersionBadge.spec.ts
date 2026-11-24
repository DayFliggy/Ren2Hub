import {
  DOMWrapper,
  flushPromises,
  mount,
  type VueWrapper,
} from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/api/types'
import CommandPaletteModal from '@/components/console/CommandPaletteModal.vue'
import ConsoleTopbar from '@/components/console/ConsoleTopbar.vue'
import { useAuthStore } from '@/stores/auth'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  status: vi.fn(),
}))
vi.mock('@/api/client', () => ({
  api: { get: mocks.get, post: mocks.post },
  setApiUnauthorizedHandler: vi.fn(),
}))
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
const COMMAND =
  'sudo -n python3 -B scripts/server/ren2hub-app.py rollback --version'

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

/** The panel is teleported to <body>, so lookups search the page. */
const body = () => new DOMWrapper(document.body)
const page = {
  get: (selector: string) => body().get(selector),
  find: (selector: string) => body().find(selector),
  findAll: (selector: string) => body().findAll(selector),
  text: () => body().text(),
  unmount: () => wrapper!.unmount(),
}

/** Mounts the real top bar, so role and account changes reach the badge
 *  through the mount boundary they take in the console. `realModals` keeps
 *  the real search dialog and ConsoleModal for keyboard order checks. */
async function setup(role = 100, { realModals = false } = {}) {
  useAuthStore().persist({ id: 1, role } as never)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'dashboard', component: { template: '<div />' } },
    ],
  })
  await router.push('/')
  wrapper = mount(ConsoleTopbar, {
    attachTo: document.body,
    global: {
      plugins: [
        router,
        createI18n({
          legacy: false,
          locale: 'en',
          // The top bar's own labels are not under test here.
          missingWarn: false,
          fallbackWarn: false,
          messages: { en: { common: { confirm: 'OK', cancel: 'Cancel' } } },
        }),
      ],
      stubs: {
        TopNavMenu: true,
        UserMenu: true,
        ConsoleMobileNavDrawer: true,
        LanguageSelector: true,
        ThemeSwitcher: true,
        ...(realModals
          ? {}
          : {
              CommandPaletteModal: true,
              ConsoleModal: {
                props: ['open'],
                template:
                  '<div v-if="open" aria-modal="true" data-modal><slot /><slot name="footer" /></div>',
              },
            }),
      },
    },
  })
  await flushPromises()
  return page
}
async function openPanel() {
  await page.get('[data-version-badge]').trigger('click')
  await flushPromises()
  return page.get('[data-version-panel]')
}
const panelState = () =>
  page.get('[data-version-panel]').attributes('data-version-state')
const tone = () =>
  page.get('[data-version-badge]').attributes('data-version-tone')
const badgeLabel = () =>
  page.get('[data-version-badge]').attributes('aria-label')
/** The second click, inside the panel where the first one was. */
const accept = () => page.get('[data-version-confirm-accept]').trigger('click')
async function confirm() {
  await accept()
  await flushPromises()
}
const errorText = () => page.get('[data-version-error]').text()
async function startUpdate() {
  await openPanel()
  await page.get('[data-version-update-now]').trigger('click')
  await confirm()
}
/** A restart is offered only once an update has replaced the program. */
async function replaceProgram() {
  replies['/api/admin/system/update'] = { need_restart: true }
  await startUpdate()
}
const restartSent = () =>
  mocks.post.mock.calls.some(([url]) => url === '/api/admin/system/restart')
async function chooseRollback(version: string) {
  await page.get('[data-version-rollback-toggle]').trigger('click')
  await flushPromises()
  await page.get(`[data-version-rollback-option="${version}"]`).trigger('click')
}

beforeEach(() => {
  vi.clearAllMocks()
  setActivePinia(createPinia())
  reload = vi.fn()
  Object.defineProperty(window, 'location', {
    value: { reload },
    configurable: true,
    writable: true,
  })
  replies = {
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

describe('version badge', () => {
  it.each([1, 10])(
    'renders nothing and reads nothing for role %s',
    async (role) => {
      const view = await setup(role)
      expect(view.find('[data-version-badge]').exists()).toBe(false)
      expect(mocks.get).not.toHaveBeenCalled()
    }
  )

  it('marks a newer release with a warning dot and names both versions', async () => {
    const view = await setup()
    expect(mocks.get).toHaveBeenCalledTimes(1)
    expect(mocks.get).toHaveBeenCalledWith(
      '/api/admin/system/check-updates',
      { force: false },
      expect.anything()
    )
    const badge = view.get('[data-version-badge]')
    // The brand shows no version text; the label and the panel carry it.
    expect(badge.text()).toBe('')
    expect(badge.attributes('aria-label')).toBe(
      'Version v1.0.1, v1.0.2 available'
    )
    expect(tone()).toBe('warning')
  })

  it('keeps the dot neutral for an up-to-date release', async () => {
    replies['/api/admin/system/check-updates'] = upToDate()
    await setup()
    expect(tone()).toBe('neutral')
    expect(badgeLabel()).toBe('Version v1.0.1, up to date')
    const panel = await openPanel()
    expect(panelState()).toBe('current')
    expect(panel.get('[data-version-current]').text()).toBe('v1.0.1')
    expect(panel.text()).toContain('Up to date')
    expect(panel.find('[data-version-update-now]').exists()).toBe(false)
    expect(panel.find('[data-version-notes]').exists()).toBe(false)
    // The rollback list is read only when its section opens.
    expect(mocks.get).toHaveBeenCalledTimes(1)
  })

  it('shows the newer release with date, changelog and collapsed notes', async () => {
    const view = await setup()
    const panel = await openPanel()
    expect(panelState()).toBe('update')
    expect(panel.get('[data-version-latest]').text()).toBe('Latest: v1.0.2')
    expect(panel.get('[data-version-update]').text()).toMatch(
      /Published .*2026/
    )
    expect(panel.get('[data-version-release-link]').attributes()).toMatchObject(
      {
        href: RELEASE_URL,
        target: '_blank',
        rel: 'noopener noreferrer',
      }
    )
    expect(panel.find('[data-version-notes]').exists()).toBe(false)
    await panel.get('[data-version-notes-toggle]').trigger('click')
    const notes = view.get('[data-version-notes]')
    expect(notes.find('h3, h4').text()).toBe('Fixes')
    expect(notes.find('strong').text()).toBe('routing')
    expect(notes.findAll('a').map((a) => a.attributes('href'))).toEqual([
      'https://example.com/docs',
      undefined,
    ])
    expect(notes.find('script').exists()).toBe(false)
  })

  it('offers no restart on an up-to-date release', async () => {
    replies['/api/admin/system/check-updates'] = upToDate()
    await setup()
    const panel = await openPanel()
    expect(panelState()).toBe('current')
    expect(panel.find('[data-version-restart]').exists()).toBe(false)
    expect(panel.findAll('button').some((b) => /restart/i.test(b.text()))).toBe(
      false
    )
  })

  it('shows a source build with a newer release as a link without update or rollback', async () => {
    replies['/api/admin/system/check-updates'] = check({ build_type: 'source' })
    const view = await setup()
    const panel = await openPanel()
    expect(panelState()).toBe('source-update')
    expect(panel.text()).toContain(
      'Source builds do not support in-app updates.'
    )
    expect(panel.get('[data-version-release-link]').attributes('href')).toBe(
      RELEASE_URL
    )
    expect(panel.find('[data-version-update-now]').exists()).toBe(false)
    expect(view.find('[data-version-rollback-toggle]').exists()).toBe(false)
  })

  it('explains that an up-to-date source build cannot roll back', async () => {
    replies['/api/admin/system/check-updates'] = check({
      build_type: 'source',
      current_version: 'dev',
      latest_version: 'v1.0.2',
      has_update: false,
    })
    const view = await setup()
    await openPanel()
    await view.get('[data-version-rollback-toggle]').trigger('click')
    await flushPromises()
    expect(view.find('[data-version-rollback-source]').exists()).toBe(true)
    expect(mocks.get).toHaveBeenCalledTimes(1)
  })

  it('shows the cached result with one translated warning line', async () => {
    replies['/api/admin/system/check-updates'] = check({
      cached: true,
      warning: 'GitHub request failed: dial tcp timeout',
    })
    await setup()
    const panel = await openPanel()
    expect(panel.get('[data-version-warning]').text()).toBe(
      'GitHub could not be reached; showing the last cached result.'
    )
    expect(panel.text()).not.toContain('dial tcp')
    expect(panelState()).toBe('update')
  })

  it('maps a failed check, including a repository without releases, to an error with retry', async () => {
    replies['/api/admin/system/check-updates'] = () => {
      throw httpError(502)
    }
    const view = await setup()
    const panel = await openPanel()
    expect(panelState()).toBe('check-error')
    expect(errorText()).toContain(
      'Could not reach GitHub for release information.'
    )
    expect(panel.text()).not.toContain('server says')
    expect(tone()).toBe('danger')
    expect(badgeLabel()).toMatch(/, could not check for updates$/)
    expect(panel.find('[data-version-update-now]').exists()).toBe(false)
    replies['/api/admin/system/check-updates'] = upToDate()
    await view.get('[data-version-retry]').trigger('click')
    await flushPromises()
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/admin/system/check-updates',
      { force: true },
      expect.anything()
    )
    expect(panelState()).toBe('current')
  })

  it('forces a check from the header and disables actions while it runs', async () => {
    const view = await setup()
    await openPanel()
    let finish: (value: unknown) => void = () => {}
    replies['/api/admin/system/check-updates'] = () =>
      new Promise((resolve) => (finish = resolve))
    await view.get('[data-version-refresh]').trigger('click')
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/admin/system/check-updates',
      { force: true },
      expect.anything()
    )
    expect(
      view.get('[data-version-refresh]').attributes('disabled')
    ).toBeDefined()
    expect(
      view.get('[data-version-update-now]').attributes('disabled')
    ).toBeDefined()
    finish(upToDate())
    await flushPromises()
    expect(panelState()).toBe('current')
    expect(
      view.get('[data-version-refresh]').attributes('disabled')
    ).toBeUndefined()
  })

  it('closes on an outside click but keeps a running update', async () => {
    let finish: (value: unknown) => void = () => {}
    replies['/api/admin/system/update'] = () =>
      new Promise((resolve) => (finish = resolve))
    const view = await setup()
    await startUpdate()
    document.body.click()
    await flushPromises()
    expect(view.find('[data-version-panel]').exists()).toBe(false)
    finish({ need_restart: true })
    await flushPromises()
    await openPanel()
    expect(panelState()).toBe('restart-needed')
    expect(mocks.post).toHaveBeenCalledTimes(1)
  })

  it('closes on Escape wherever focus is and returns focus to the badge', async () => {
    const view = await setup()
    await openPanel()
    // A click on panel text leaves focus on the page, not in the badge.
    ;(document.activeElement as HTMLElement | null)?.blur()
    document.body.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
    )
    await flushPromises()
    expect(view.find('[data-version-panel]').exists()).toBe(false)
    expect(document.activeElement).toBe(
      view.get('[data-version-badge]').element
    )
  })

  it('asks to restart after a confirmed update and clears the cached check', async () => {
    replies['/api/admin/system/update'] = { need_restart: true }
    const view = await setup()
    await openPanel()
    await view.get('[data-version-update-now]').trigger('click')
    expect(mocks.post).not.toHaveBeenCalled()
    await confirm()
    expect(mocks.post).toHaveBeenCalledWith(
      '/api/admin/system/update',
      undefined,
      expect.objectContaining({ timeoutMs: 330_000 })
    )
    expect(panelState()).toBe('restart-needed')
    // The update answer does not name the installed release, so neither does the UI.
    const notice = view.get('[data-version-restart-needed]').text()
    expect(notice).toContain('The program was replaced. Restart to apply it.')
    expect(notice).not.toContain('v1.0.2')
    expect(tone()).toBe('warning')
    // The cleared check leaves the running version to /api/status, unset here.
    expect(badgeLabel()).toMatch(/, replaced; restart to apply$/)
    expect(view.find('[data-version-update-now]').exists()).toBe(false)
  })

  it('reads the check again when the server is already up to date', async () => {
    replies['/api/admin/system/update'] = {
      already_up_to_date: true,
      current_version: 'v1.0.2',
      latest_version: 'v1.0.2',
    }
    await setup()
    replies['/api/admin/system/check-updates'] = check({
      current_version: 'v1.0.2',
      has_update: false,
    })
    await startUpdate()
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/admin/system/check-updates',
      { force: true },
      expect.anything()
    )
    expect(panelState()).toBe('current')
    expect(tone()).toBe('neutral')
    expect(badgeLabel()).toBe('Version v1.0.2, up to date')
  })

  it('ignores a second update while the first runs', async () => {
    let finish: (value: unknown) => void = () => {}
    replies['/api/admin/system/update'] = () =>
      new Promise((resolve) => (finish = resolve))
    const view = await setup()
    await startUpdate()
    // The second step stays while the update runs, with its buttons off.
    expect(view.find('[data-version-update-now]').exists()).toBe(false)
    expect(
      view.get('[data-version-confirm-accept]').attributes('disabled')
    ).toBeDefined()
    expect(
      view.get('[data-version-confirm-cancel]').attributes('disabled')
    ).toBeDefined()
    expect(view.get('[data-version-panel]').text()).toContain(
      'Downloading, verifying and replacing the program'
    )
    await accept()
    await flushPromises()
    expect(mocks.post).toHaveBeenCalledTimes(1)
    finish({ need_restart: true })
    await flushPromises()
  })

  it.each([
    [400, 'The server refused the request'],
    [409, 'Another update, rollback or restart is in progress.'],
    [502, 'Downloading or verifying the release package failed.'],
    [500, 'The server could not complete the operation'],
    [504, 'do not click Update again'],
    [undefined, 'do not click Update again'],
    [418, 'The request failed (HTTP 418).'],
  ])('maps update failures by HTTP status %s', async (status, text) => {
    replies['/api/admin/system/update'] = () => {
      throw status
        ? httpError(status)
        : new ApiError('timeout of 330000ms exceeded')
    }
    const view = await setup()
    await startUpdate()
    expect(panelState()).toBe('action-error')
    expect(errorText()).toContain(text)
    expect(tone()).toBe('danger')
    // The server may have failed to restore the old binary as well.
    expect(errorText()).not.toContain('restored')
    expect(view.text()).not.toContain('server says')
    // The next step re-reads the server instead of offering the update again.
    expect(view.get('[data-version-retry]').text()).toBe(
      'Refresh version information'
    )
    expect(view.find('[data-version-restart-needed]').exists()).toBe(false)
  })

  it('reports a changed sign-in instead of a running operation', async () => {
    replies['/api/admin/system/update'] = () => {
      throw new ApiError('session changed', {
        status: 409,
        code: 'AUTH_SESSION_CHANGED',
      })
    }
    await setup()
    await startUpdate()
    expect(errorText()).toContain('Your sign-in changed')
    expect(errorText()).not.toContain('in progress')
  })

  it('reads the rollback list on expand and shows it as dated radios', async () => {
    replies['/api/admin/system/check-updates'] = upToDate()
    const view = await setup()
    await openPanel()
    await view.get('[data-version-rollback-toggle]').trigger('click')
    await flushPromises()
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/admin/system/rollback-versions',
      undefined,
      expect.anything()
    )
    const options = view.findAll('[role="radio"]')
    expect(options.map((o) => o.text())).toEqual([
      expect.stringMatching(/^v1\.0\.0.*2026/),
      expect.stringMatching(/^v0\.9\.9.*2026/),
    ])
    expect(view.find('[data-version-rollback-confirm]').exists()).toBe(false)
    await options[1]!.trigger('click')
    expect(options[1]!.attributes('aria-checked')).toBe('true')
    expect(view.text()).toContain(
      'An in-app rollback does not back up the database.'
    )
    expect(view.get('[data-version-rollback-command]').text()).toBe(
      `${COMMAND} v0.9.9`
    )
    expect(view.get('[data-version-rollback-confirm]').text()).toBe(
      'Roll back to v0.9.9'
    )
  })

  it('copies the rollback command', async () => {
    replies['/api/admin/system/check-updates'] = upToDate()
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    })
    const view = await setup()
    await openPanel()
    await chooseRollback('1.0.0')
    await view.get('[data-version-rollback-copy]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(`${COMMAND} v1.0.0`)
    expect(view.get('[data-version-rollback]').text()).toContain('Copied')
  })

  it('shows an empty rollback list', async () => {
    replies['/api/admin/system/check-updates'] = upToDate()
    replies['/api/admin/system/rollback-versions'] = { versions: [] }
    const view = await setup()
    await openPanel()
    await view.get('[data-version-rollback-toggle]').trigger('click')
    await flushPromises()
    expect(view.get('[data-version-rollback]').text()).toBe(
      'No earlier release is available.'
    )
    expect(view.find('[data-version-rollback-confirm]').exists()).toBe(false)
  })

  it('retries a failed rollback list', async () => {
    replies['/api/admin/system/check-updates'] = upToDate()
    replies['/api/admin/system/rollback-versions'] = () => {
      throw httpError(502)
    }
    const view = await setup()
    await openPanel()
    await view.get('[data-version-rollback-toggle]').trigger('click')
    await flushPromises()
    expect(view.get('[data-version-rollback]').text()).toContain(
      'Could not reach GitHub'
    )
    replies['/api/admin/system/rollback-versions'] = { versions: [] }
    await view.get('[data-version-rollback-retry]').trigger('click')
    await flushPromises()
    expect(view.get('[data-version-rollback]').text()).toBe(
      'No earlier release is available.'
    )
  })

  it('rolls back to the selected version and asks to restart', async () => {
    replies['/api/admin/system/check-updates'] = upToDate()
    replies['/api/admin/system/rollback'] = {
      need_restart: true,
      version: '1.0.0',
    }
    const view = await setup()
    await openPanel()
    await chooseRollback('1.0.0')
    await view.get('[data-version-rollback-confirm]').trigger('click')
    await flushPromises()
    expect(mocks.post).toHaveBeenCalledWith(
      '/api/admin/system/rollback',
      { version: '1.0.0' },
      expect.objectContaining({ timeoutMs: 330_000 })
    )
    expect(view.find('[data-version-rollback]').exists()).toBe(false)
    expect(view.get('[data-version-restart-needed]').text()).toContain(
      'Rolled back to v1.0.0. Restart to apply it.'
    )
  })

  it('maps a rollback that is not in the list', async () => {
    replies['/api/admin/system/check-updates'] = upToDate()
    replies['/api/admin/system/rollback'] = () => {
      throw httpError(404)
    }
    const view = await setup()
    await openPanel()
    await chooseRollback('0.9.9')
    await view.get('[data-version-rollback-confirm]').trigger('click')
    await flushPromises()
    expect(errorText()).toContain('This version is not in the rollback list.')
    expect(view.find('[data-version-restart-needed]').exists()).toBe(false)
  })

  it('reloads after an update once the server reports a different version', async () => {
    replies['/api/admin/system/update'] = { need_restart: true }
    replies['/api/admin/system/restart'] = { restarting: true }
    const view = await setup()
    await startUpdate()
    vi.useFakeTimers()
    // v1.0.3 was published after the check that offered v1.0.2.
    mocks.status
      .mockResolvedValueOnce({ version: 'v1.0.1', start_time: 100 })
      .mockRejectedValueOnce(new ApiError('Bad Gateway', { status: 502 }))
      .mockResolvedValueOnce({ version: 'v1.0.1', start_time: 150 })
      .mockResolvedValue({ version: 'v1.0.3', start_time: 200 })
    await view.get('[data-version-restart]').trigger('click')
    await accept()
    await vi.advanceTimersByTimeAsync(0)
    expect(mocks.post).toHaveBeenLastCalledWith(
      '/api/admin/system/restart',
      undefined,
      undefined
    )
    expect(panelState()).toBe('restarting')
    expect(view.get('[data-version-panel]').text()).toContain('Restarting')
    await vi.advanceTimersByTimeAsync(4_000)
    expect(reload).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(6_000)
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('reloads after a rollback only once the rollback target answers', async () => {
    replies['/api/admin/system/check-updates'] = upToDate()
    replies['/api/admin/system/rollback'] = {
      need_restart: true,
      version: '1.0.0',
    }
    replies['/api/admin/system/restart'] = { restarting: true }
    const view = await setup()
    await openPanel()
    await chooseRollback('1.0.0')
    await view.get('[data-version-rollback-confirm]').trigger('click')
    await flushPromises()
    vi.useFakeTimers()
    mocks.status
      .mockResolvedValueOnce({ version: 'v1.0.1', start_time: 100 })
      .mockResolvedValueOnce({ version: 'v1.0.2', start_time: 150 })
      .mockResolvedValue({ version: 'v1.0.0', start_time: 200 })
    await view.get('[data-version-restart]').trigger('click')
    await accept()
    await vi.advanceTimersByTimeAsync(2_500)
    expect(reload).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(2_000)
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('keeps watching after a dropped restart POST', async () => {
    replies['/api/admin/system/restart'] = () => {
      throw new ApiError('Network Error')
    }
    const view = await setup()
    await replaceProgram()
    vi.useFakeTimers()
    mocks.status
      .mockResolvedValueOnce({ version: 'v1.0.1', start_time: 100 })
      .mockResolvedValue({ version: 'v1.0.2', start_time: 300 })
    await view.get('[data-version-restart]').trigger('click')
    await accept()
    await vi.advanceTimersByTimeAsync(0)
    expect(view.find('[data-version-error]').exists()).toBe(false)
    expect(panelState()).toBe('restarting')
    await vi.advanceTimersByTimeAsync(2_000)
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('keeps the page when a restart is refused', async () => {
    replies['/api/admin/system/restart'] = () => {
      throw httpError(409)
    }
    const view = await setup()
    await replaceProgram()
    await view.get('[data-version-restart]').trigger('click')
    await confirm()
    expect(errorText()).toContain(
      'Another update, rollback or restart is in progress.'
    )
    expect(panelState()).toBe('action-error')
    // Only the baseline read before the POST; no restart polling follows.
    expect(mocks.status).toHaveBeenCalledTimes(1)
  })

  it('reports an unknown state when the restart is not observed in 120 seconds', async () => {
    replies['/api/admin/system/restart'] = { restarting: true }
    const view = await setup()
    await replaceProgram()
    vi.useFakeTimers()
    await view.get('[data-version-restart]').trigger('click')
    await accept()
    await vi.advanceTimersByTimeAsync(125_000)
    expect(view.get('[data-version-restart-unknown]').text()).toContain(
      'could not be confirmed within 120 seconds'
    )
    expect(reload).not.toHaveBeenCalled()
    await view
      .get('[data-version-panel] [role="status"] button')
      .trigger('click')
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('does not send the restart when the badge unmounts before the baseline read answers', async () => {
    replies['/api/admin/system/restart'] = { restarting: true }
    const view = await setup()
    await replaceProgram()
    let answer: (value: unknown) => void = () => {}
    mocks.status.mockImplementationOnce(
      () => new Promise((resolve) => (answer = resolve))
    )
    await view.get('[data-version-restart]').trigger('click')
    await accept()
    await flushPromises()
    view.unmount()
    wrapper = undefined
    answer({ version: 'v1.0.1', start_time: 100 })
    await flushPromises()
    expect(restartSent()).toBe(false)
  })

  it('does not reload when a success arrives after the badge unmounted', async () => {
    replies['/api/admin/system/restart'] = { restarting: true }
    const view = await setup()
    await replaceProgram()
    vi.useFakeTimers()
    let answer: (value: unknown) => void = () => {}
    mocks.status
      .mockResolvedValueOnce({ version: 'v1.0.1', start_time: 100 })
      .mockImplementationOnce(
        () => new Promise((resolve) => (answer = resolve))
      )
    await view.get('[data-version-restart]').trigger('click')
    await accept()
    await vi.advanceTimersByTimeAsync(2_000)
    expect(mocks.status).toHaveBeenCalledTimes(2)
    view.unmount()
    wrapper = undefined
    answer({ version: 'v1.0.2', start_time: 300 })
    await vi.advanceTimersByTimeAsync(10_000)
    expect(reload).not.toHaveBeenCalled()
  })

  it('ignores an update answer that arrives after the badge unmounted', async () => {
    let finish: (value: unknown) => void = () => {}
    replies['/api/admin/system/update'] = () =>
      new Promise((resolve) => (finish = resolve))
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const view = await setup()
    await startUpdate()
    view.unmount()
    wrapper = undefined
    finish({ need_restart: true })
    await flushPromises()
    expect(warn).not.toHaveBeenCalled()
    // The loaded check stays in the store; a new badge does not read again.
    await setup()
    expect(mocks.get).toHaveBeenCalledTimes(1)
    warn.mockRestore()
  })

  it('confirms an update in the panel, not in a dialog', async () => {
    const view = await setup()
    await openPanel()
    await view.get('[data-version-update-now]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[aria-modal="true"]')).toBeNull()
    const step = view.get('[data-version-panel] [data-version-confirm]')
    expect(step.text()).toContain('Update to v1.0.2?')
    expect(step.text()).toContain('A restart is needed to apply it.')
    expect(view.find('[data-version-update-now]').exists()).toBe(false)
    // Focus starts on Cancel, so a repeated Enter does not update.
    expect(document.activeElement).toBe(
      view.get('[data-version-confirm-cancel]').element
    )
    await view.get('[data-version-confirm-cancel]').trigger('click')
    await flushPromises()
    expect(mocks.post).not.toHaveBeenCalled()
    expect(view.find('[data-version-confirm]').exists()).toBe(false)
    expect(document.activeElement).toBe(
      view.get('[data-version-update-now]').element
    )
  })

  it('cancels the second step on Escape before closing the panel', async () => {
    const view = await setup(100, { realModals: true })
    await openPanel()
    await view.get('[data-version-update-now]').trigger('click')
    await flushPromises()
    expect(view.find('[data-version-confirm]').exists()).toBe(true)
    document.body.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
    )
    await flushPromises()
    expect(view.find('[data-version-panel]').exists()).toBe(true)
    expect(view.find('[data-version-confirm]').exists()).toBe(false)
    expect(document.activeElement).toBe(
      view.get('[data-version-update-now]').element
    )
    document.body.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
    )
    await flushPromises()
    expect(view.find('[data-version-panel]').exists()).toBe(false)
    expect(mocks.post).not.toHaveBeenCalled()
  })

  it('drops an unconfirmed step when the panel closes', async () => {
    const view = await setup()
    await openPanel()
    await view.get('[data-version-update-now]').trigger('click')
    await flushPromises()
    await view.get('[data-version-badge]').trigger('click')
    await openPanel()
    expect(view.find('[data-version-confirm]').exists()).toBe(false)
    expect(view.find('[data-version-update-now]').exists()).toBe(true)
  })

  it('leaves Escape to a search dialog opened over the panel', async () => {
    const view = await setup(100, { realModals: true })
    await openPanel()
    // The shortcut opens search without a click, so the panel stays open.
    document.body.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true })
    )
    await flushPromises()
    // The dialog's leave transition keeps its node for a while; its open
    // prop is what Escape changes.
    const searchOpen = () =>
      wrapper!.findComponent(CommandPaletteModal).props('open')
    expect(searchOpen()).toBe(true)
    expect(document.querySelector('[aria-modal="true"]')).not.toBeNull()
    const target = document.activeElement ?? document.body
    target.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
        cancelable: true,
      })
    )
    await flushPromises()
    expect(searchOpen()).toBe(false)
    expect(view.find('[data-version-panel]').exists()).toBe(true)
  })

  it('shows a failed refresh after the program was replaced and keeps the restart', async () => {
    replies['/api/admin/system/update'] = { need_restart: true }
    const view = await setup()
    await startUpdate()
    replies['/api/admin/system/check-updates'] = () => {
      throw httpError(502)
    }
    await view.get('[data-version-refresh]').trigger('click')
    await flushPromises()
    expect(panelState()).toBe('check-error')
    expect(errorText()).toContain('GitHub')
    expect(view.get('[data-version-restart-needed]').text()).toContain(
      'The program was replaced. Restart to apply it.'
    )
    expect(view.find('[data-version-restart]').exists()).toBe(true)
    replies['/api/admin/system/check-updates'] = check({
      current_version: 'v1.0.1',
    })
    await view.get('[data-version-retry]').trigger('click')
    await flushPromises()
    expect(panelState()).toBe('restart-needed')
    expect(view.find('[data-version-error]').exists()).toBe(false)
    expect(view.find('[data-version-restart]').exists()).toBe(true)
  })
})

describe('version badge account boundary', () => {
  it('sends no restart when Root is lost before the baseline read answers', async () => {
    replies['/api/admin/system/restart'] = { restarting: true }
    const view = await setup()
    await replaceProgram()
    let answer: (value: unknown) => void = () => {}
    mocks.status.mockImplementationOnce(
      () => new Promise((resolve) => (answer = resolve))
    )
    await view.get('[data-version-restart]').trigger('click')
    await accept()
    await flushPromises()
    useAuthStore().persist({ id: 1, role: 1 } as never)
    await flushPromises()
    expect(view.find('[data-version-badge]').exists()).toBe(false)
    answer({ version: 'v1.0.1', start_time: 100 })
    await flushPromises()
    expect(restartSent()).toBe(false)
  })

  it('does not reload for the old account once another account signs in', async () => {
    replies['/api/admin/system/restart'] = { restarting: true }
    const view = await setup()
    await replaceProgram()
    vi.useFakeTimers()
    let answer: (value: unknown) => void = () => {}
    mocks.status
      .mockResolvedValueOnce({ version: 'v1.0.1', start_time: 100 })
      .mockImplementationOnce(
        () => new Promise((resolve) => (answer = resolve))
      )
    await view.get('[data-version-restart]').trigger('click')
    await accept()
    await vi.advanceTimersByTimeAsync(2_000)
    expect(mocks.status).toHaveBeenCalledTimes(2)
    useAuthStore().persist({ id: 2, role: 100 } as never)
    await vi.advanceTimersByTimeAsync(0)
    answer({ version: 'v1.0.2', start_time: 300 })
    await vi.advanceTimersByTimeAsync(10_000)
    expect(reload).not.toHaveBeenCalled()
  })

  it('starts over for another Root account', async () => {
    replies['/api/admin/system/update'] = { need_restart: true }
    const view = await setup()
    await startUpdate()
    expect(panelState()).toBe('restart-needed')
    expect(mocks.get).toHaveBeenCalledTimes(1)
    replies['/api/admin/system/check-updates'] = upToDate()
    useAuthStore().persist({ id: 2, role: 100 } as never)
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledTimes(2)
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/admin/system/check-updates',
      { force: false },
      expect.anything()
    )
    await openPanel()
    expect(panelState()).toBe('current')
    expect(view.find('[data-version-restart-needed]').exists()).toBe(false)
  })
})
