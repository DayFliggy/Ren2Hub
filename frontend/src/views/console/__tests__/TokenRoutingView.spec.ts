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
  previewToken: vi.fn(),
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
    request_models: ['gpt-5'],
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
    request_models: ['gpt-5'],
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
      diagnostics: [],
      conversion_plan: {
        from: 'openai',
        to: 'openai',
        native: true,
        policy: 'safe',
        stream_supported: true,
        steps: [],
        diagnostics: [],
      },
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
  conversion_policy: 'safe',
  request_requirements: {
    format: 'openai',
    model: 'gpt-5',
    stream: false,
    operation: 'text',
    features: [],
    native_required: false,
    native_state: [],
    tools: [],
    diagnostics: [],
  },
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
  mocks.previewToken.mockResolvedValue(preview)
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
  it('previews an automatic token without creating a profile', async () => {
    mocks.profiles.mockResolvedValueOnce([])
    const view = await mountView()
    await view.get('input[placeholder="gpt-5"]').setValue('gpt-5')
    await buttonByText(view, 'Run preview').trigger('click')
    await flushPromises()
    expect(mocks.previewToken).toHaveBeenCalledWith(
      11,
      { model: 'gpt-5', path: '/v1/chat/completions' },
      expect.any(AbortSignal)
    )
    expect(mocks.create).not.toHaveBeenCalled()
  })
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

    expect(mocks.previewToken).toHaveBeenCalledWith(
      11,
      {
        model: 'gpt-5',
        path: '/v1/chat/completions',
      },
      expect.any(AbortSignal)
    )
    expect(view.text()).toContain('Cooling down')
    expect(view.get('[data-testid="route-preview-health"]').text()).toContain(
      '3 failures'
    )
  })

  it('clears a previous preview when the next preview fails', async () => {
    const view = await mountView()

    await view.get('input[placeholder="gpt-5"]').setValue('gpt-5')
    await buttonByText(view, 'Run preview').trigger('click')
    await flushPromises()
    expect(view.find('[data-testid="route-preview-health"]').exists()).toBe(
      true
    )

    mocks.previewToken.mockRejectedValueOnce(new ApiError('preview failed'))
    await buttonByText(view, 'Run preview').trigger('click')
    await flushPromises()

    expect(view.find('[data-testid="route-preview-health"]').exists()).toBe(
      false
    )
  })

  it('shows request-wide rejection reasons when the preview has no entries', async () => {
    const blocked = structuredClone(preview)
    blocked.entries = []
    blocked.candidate_channel_ids = []
    delete blocked.preferred_channel_id
    blocked.filter_reason_counts = { profile_disabled: 1 }
    mocks.previewToken.mockResolvedValueOnce(blocked)
    const view = await mountView()
    await view.get('input[placeholder="gpt-5"]').setValue('gpt-5')
    await buttonByText(view, 'Run preview').trigger('click')
    await flushPromises()
    expect(view.text()).toContain('profile_disabled')
  })

  it('sends the request sample and renders blocked conversion diagnostics', async () => {
    const blocked = structuredClone(preview)
    blocked.request_requirements.stream = true
    blocked.request_requirements.features = ['serial_tools']
    blocked.candidate_channel_ids = []
    delete blocked.preferred_channel_id
    blocked.entries[0].filter_reason = 'conversion_unavailable'
    blocked.entries[0].conversion_plan = {
      from: 'openai',
      to: 'gemini',
      native: false,
      policy: 'safe',
      stream_supported: true,
      diagnostics: [],
      steps: [{ converter: 'openai_to_gemini', from: 'openai', to: 'gemini' }],
    }
    blocked.entries[0].diagnostics = [
      {
        code: 'serial_tools_not_portable',
        path: 'parallel_tool_calls',
        message: 'Gemini conversion cannot enforce serial tool execution',
        severity: 'error',
        from: 'openai',
        to: 'gemini',
      },
    ]
    mocks.previewToken.mockResolvedValueOnce(blocked)
    const view = await mountView()
    await view.get('input[placeholder="gpt-5"]').setValue('gpt-5')
    await view
      .get('[data-testid="route-preview-request"]')
      .setValue('{"stream":true,"parallel_tool_calls":false}')
    await buttonByText(view, 'Run preview').trigger('click')
    await flushPromises()
    expect(mocks.previewToken).toHaveBeenCalledWith(
      11,
      {
        model: 'gpt-5',
        path: '/v1/chat/completions',
        request: { stream: true, parallel_tool_calls: false },
      },
      expect.any(AbortSignal)
    )
    expect(
      view.get('[data-testid="route-preview-requirements"]').text()
    ).toContain('Streaming')
    expect(
      view.get('[data-testid="route-preview-conversion"]').text()
    ).toContain('openai_to_gemini')
    expect(
      view.get('[data-testid="route-preview-diagnostics"]').text()
    ).toContain('parallel_tool_calls')
    expect(view.text()).toContain('Conversion unavailable')
    await view.get('[data-testid="route-preview-request"]').setValue('{}')
    expect(view.find('[data-testid="route-preview-conversion"]').exists()).toBe(
      false
    )
  })

  it('rejects invalid request samples before calling the preview endpoint', async () => {
    const view = await mountView()
    await view.get('input[placeholder="gpt-5"]').setValue('gpt-5')
    for (const sample of ['{', 'null', '[]', 'true']) {
      await view.get('[data-testid="route-preview-request"]').setValue(sample)
      await buttonByText(view, 'Run preview').trigger('click')
      await flushPromises()
    }
    expect(mocks.previewToken).not.toHaveBeenCalled()
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

  it('deletes an existing profile and clears the editor', async () => {
    const view = await mountView()

    await buttonByText(view, 'Delete profile').trigger('click')
    await flushPromises()

    expect(mocks.remove).toHaveBeenCalledWith(1)
    expect(view.text()).toContain('Run preview')
  })

  it('does not apply a preview that resolves after the profile is deleted', async () => {
    let resolvePreview: (value: RoutePreview) => void = () => undefined
    mocks.previewToken.mockImplementationOnce(
      () =>
        new Promise<RoutePreview>((resolve) => {
          resolvePreview = resolve
        })
    )
    const view = await mountView()

    await view.get('input[placeholder="gpt-5"]').setValue('gpt-5')
    await buttonByText(view, 'Run preview').trigger('click')
    await buttonByText(view, 'Delete profile').trigger('click')
    resolvePreview(preview)
    await flushPromises()

    expect(view.text()).toContain('Run preview')
    expect(view.find('[data-testid="route-preview-health"]').exists()).toBe(
      false
    )
  })
})
