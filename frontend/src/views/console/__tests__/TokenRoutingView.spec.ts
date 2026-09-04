import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'

import { ApiError } from '@/api/types'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type {
  EligibleRouteChannel,
  RouteCatalog,
  RoutePreview,
  RouteProfileView,
} from '@/types/routing'
import TokenRoutingView from '@/views/console/TokenRoutingView.vue'

const mocks = vi.hoisted(() => ({
  profiles: vi.fn(),
  eligibleChannels: vi.fn(),
  catalog: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  preview: vi.fn(),
}))

vi.mock('@/api/routingApi', () => ({
  routingApi: mocks,
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
  }),
}))

const profile: RouteProfileView = {
  profile: {
    id: 1,
    user_id: 7,
    token_id: 11,
    mode: 'manual',
    active_group_id: 4,
    version: 3,
    status: 1,
    created_at: 1,
    updated_at: 2,
  },
  groups: [
    {
      id: 4,
      profile_id: 1,
      name: 'Primary',
      kind: 'manual',
      enabled: true,
      position: 0,
      entries: [
        {
          id: 9,
          group_id: 4,
          channel_id: 101,
          source: 'platform',
          enabled: true,
          position: 0,
          weight: 100,
        },
        {
          id: 10,
          group_id: 4,
          channel_id: 102,
          source: 'platform',
          enabled: true,
          position: 1,
          weight: 100,
        },
      ],
      policy: {
        group_id: 4,
        load_balance: false,
        max_ratio: 1,
        retry_mode: 'next_channel',
        max_same_resource_attempts: 0,
        max_failover_attempts: 1,
        sticky: false,
      },
    },
  ],
}

const channels: EligibleRouteChannel[] = [
  {
    id: 101,
    name: 'Primary channel',
    type: 1,
    status: 1,
    models: 'gpt-5',
    priority: 1,
    weight: 1,
    snapshot_version: 1,
    catalog_version: 'catalog-1',
    capability_state: 'eligible',
  },
  {
    id: 102,
    name: 'Backup channel',
    type: 1,
    status: 1,
    models: 'gpt-5',
    priority: 1,
    weight: 1,
    snapshot_version: 1,
    catalog_version: 'catalog-1',
    capability_state: 'eligible',
  },
]

const catalog: RouteCatalog = {
  catalog_version: 'catalog-1',
  catalog_versions: ['catalog-1'],
  items: [
    {
      id: 1,
      channel_id: 101,
      request_model: 'gpt-5',
      actual_model: 'gpt-5',
      lab_slug: 'openai',
      confidence: 1,
      source: 'canonical',
      catalog_version: 'catalog-1',
      snapshot_version: 1,
      state: 'eligible',
    },
  ],
}

const preview: RoutePreview = {
  profile_id: 1,
  profile_version: 3,
  request_model: 'gpt-5',
  normalized_model: 'gpt-5',
  path: '/v1/chat/completions',
  endpoint_type: 'openai',
  entries: [
    {
      entry_id: 9,
      channel_id: 101,
      position: 0,
      weight: 100,
      request_model: 'gpt-5',
      actual_model: 'gpt-5',
      lab_slug: 'openai',
      snapshot_version: 1,
      catalog_version: 'catalog-1',
      capability_state: 'eligible',
      health: {
        state: 'open',
        failure_count: 3,
        cooldown_until: 1_800_000_000,
        health_epoch: 4,
        last_latency_ms: 120,
        first_token_latency_ms: 45,
        updated_at: 1_700_000_100,
      },
    },
  ],
  candidate_channel_ids: [101],
  selection_mode: 'ordered',
  preferred_channel_id: 101,
  filter_reason_counts: {},
  has_mixed: false,
  runtime_recheck_required: true,
  runtime_recheck_reasons: ['price_qualification'],
  live_selection: false,
}

const stubs = {
  PageBreadcrumb: { template: '<div><slot name="action" /></div>' },
  ConsoleCard: {
    template: '<section><slot name="action" /><slot /></section>',
  },
  ConsoleButton: {
    props: ['disabled', 'loading'],
    emits: ['click'],
    template:
      '<button :disabled="disabled" @click="$emit(\'click\')"><slot /></button>',
  },
  EmptyState: { template: '<div><slot /></div>' },
  FormField: { template: '<label><slot /></label>' },
  StatusChip: { template: '<span><slot /></span>' },
  TextInput: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    template:
      '<input v-bind="$attrs" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
  },
}

let wrapper: VueWrapper | null = null

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

beforeEach(() => {
  vi.clearAllMocks()
  mocks.profiles.mockResolvedValue([profile])
  mocks.eligibleChannels.mockResolvedValue(channels)
  mocks.catalog.mockResolvedValue(catalog)
  mocks.update.mockResolvedValue(profile)
  mocks.remove.mockResolvedValue(undefined)
  mocks.preview.mockResolvedValue(preview)
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

async function mountView(): Promise<VueWrapper> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/console/keys/:id/routing', component: TokenRoutingView },
    ],
  })
  await router.push('/console/keys/11/routing')
  await router.isReady()
  wrapper = mount(TokenRoutingView, {
    global: {
      plugins: [i18n, router],
      stubs,
    },
  })
  await flushPromises()
  return wrapper
}

function buttonByText(view: VueWrapper, text: string) {
  const button = view
    .findAll('button')
    .find((item) => item.text().includes(text))
  if (!button) throw new Error(`Missing button: ${text}`)
  return button
}

describe('TokenRoutingView', () => {
  it('saves reordered entries by stable channel id', async () => {
    const view = await mountView()

    await view.get('button[aria-label="Move down"]').trigger('click')
    await buttonByText(view, 'Save').trigger('click')
    await flushPromises()

    expect(mocks.update).toHaveBeenCalledWith(
      1,
      expect.objectContaining({
        version: 3,
        groups: [
          expect.objectContaining({
            entries: [
              expect.objectContaining({ channel_id: 102, position: 0 }),
              expect.objectContaining({ channel_id: 101, position: 1 }),
            ],
          }),
        ],
      })
    )
  })

  it('renders the model-scoped preview health summary', async () => {
    const view = await mountView()

    await view.get('input[placeholder="gpt-5"]').setValue('gpt-5')
    await buttonByText(view, 'Run preview').trigger('click')
    await flushPromises()

    expect(mocks.preview).toHaveBeenCalledWith(1, {
      model: 'gpt-5',
      path: '/v1/chat/completions',
    })
    expect(view.text()).toContain('Cooling down')
    expect(view.get('[data-testid="route-preview-health"]').text()).toContain(
      '3 failures'
    )
  })

  it('requires a reload after a version conflict', async () => {
    mocks.update.mockRejectedValueOnce(
      new ApiError('stale profile', { code: 'VERSION_CONFLICT' })
    )
    const view = await mountView()

    await buttonByText(view, 'Save').trigger('click')
    await flushPromises()

    expect(view.text()).toContain('cannot be saved')
    expect(buttonByText(view, 'Save').attributes('disabled')).toBeDefined()
    expect(buttonByText(view, 'Delete').attributes('disabled')).toBeDefined()
    await buttonByText(view, 'Delete').trigger('click')
    expect(mocks.remove).not.toHaveBeenCalled()
    await buttonByText(view, 'Retry').trigger('click')
    await flushPromises()
    expect(mocks.profiles).toHaveBeenCalledTimes(2)
  })

  it('does not save a stale draft after loading fails', async () => {
    mocks.profiles.mockRejectedValueOnce(new ApiError('load failed'))
    const view = await mountView()

    const save = buttonByText(view, 'Save')
    expect(save.attributes('disabled')).toBeDefined()
    await save.trigger('click')

    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.update).not.toHaveBeenCalled()
  })

  it('deletes the current profile and returns to the create state', async () => {
    const view = await mountView()

    await buttonByText(view, 'Delete').trigger('click')
    await flushPromises()

    expect(mocks.remove).toHaveBeenCalledWith(1)
    expect(view.text()).toContain('This token has no routing profile yet')
  })

  it('clears drag state when a drag ends without a drop', async () => {
    const view = await mountView()
    const entries = view.findAll('[draggable="true"]')

    await entries[0].trigger('dragstart')
    await entries[0].trigger('dragend')
    await entries[1].trigger('drop')
    await buttonByText(view, 'Save').trigger('click')
    await flushPromises()

    expect(mocks.update).toHaveBeenCalledWith(
      1,
      expect.objectContaining({
        groups: [
          expect.objectContaining({
            entries: [
              expect.objectContaining({ channel_id: 101, position: 0 }),
              expect.objectContaining({ channel_id: 102, position: 1 }),
            ],
          }),
        ],
      })
    )
  })

  it('shows channel capability for the requested model', async () => {
    mocks.catalog.mockResolvedValue({
      ...catalog,
      items: [
        { ...catalog.items[0], request_model: 'gpt-4', lab_slug: 'wrong-lab' },
        { ...catalog.items[0], request_model: 'gpt-5', lab_slug: 'right-lab' },
      ],
    })
    const view = await mountView()

    await view.get('input[placeholder="gpt-5"]').setValue('gpt-5')

    expect(view.text()).toContain('right-lab')
    expect(view.text()).not.toContain('wrong-lab')
  })

  it('normalizes the requested model before matching catalog capability', async () => {
    const view = await mountView()

    await view.get('input[placeholder="gpt-5"]').setValue('GPT-5')

    expect(view.text()).toContain('openai')
  })

  it('keeps system-managed auto Lab profiles read-only', async () => {
    mocks.profiles.mockResolvedValue([
      {
        ...profile,
        profile: { ...profile.profile, mode: 'auto_lab' },
        groups: [{ ...profile.groups[0], kind: 'auto_lab' }],
      },
    ])
    const view = await mountView()

    expect(buttonByText(view, 'Save').attributes('disabled')).toBeDefined()
    expect(buttonByText(view, 'Delete').attributes('disabled')).toBeDefined()
    await buttonByText(view, 'Save').trigger('click')
    await buttonByText(view, 'Delete').trigger('click')

    expect(mocks.update).not.toHaveBeenCalled()
    expect(mocks.remove).not.toHaveBeenCalled()
  })

  it('reloads on token changes and ignores the previous token response', async () => {
    let resolveFirst: ((value: RouteProfileView[]) => void) | undefined
    const firstProfiles = new Promise<RouteProfileView[]>((resolve) => {
      resolveFirst = resolve
    })
    const secondProfile = {
      ...profile,
      profile: { ...profile.profile, token_id: 12 },
      groups: [{ ...profile.groups[0], name: 'Secondary' }],
    }
    mocks.profiles
      .mockReset()
      .mockImplementationOnce(() => firstProfiles)
      .mockResolvedValueOnce([secondProfile])

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/console/keys/:id/routing', component: TokenRoutingView },
      ],
    })
    await router.push('/console/keys/11/routing')
    await router.isReady()
    wrapper = mount(TokenRoutingView, {
      global: {
        plugins: [i18n, router],
        stubs,
      },
    })
    await flushPromises()

    await router.push('/console/keys/12/routing')
    await flushPromises()
    await nextTick()

    expect(mocks.profiles).toHaveBeenCalledTimes(2)
    await nextTick()
    expect(
      wrapper.find('input[aria-label="Group name"]').element
    ).toHaveProperty('value', 'Secondary')

    resolveFirst?.([profile])
    await flushPromises()
    await nextTick()

    await nextTick()
    expect(
      wrapper.find('input[aria-label="Group name"]').element
    ).toHaveProperty('value', 'Secondary')
  })
})
