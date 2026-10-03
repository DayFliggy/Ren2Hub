import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'

import VendorRouteGroup from '@/components/console/dashboard/autoroute/VendorRouteGroup.vue'
import { labLogoVendor, vendorLogoMeta } from '@/constants/console'
import type { RouteChannelRow } from '@/composables/useAutoRoute'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { RouteHealthSummary } from '@/utils/routeHealth'
import { scoreChannels, type ChannelRoutingMetrics } from '@/utils/routeScore'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

function makeChannel(
  overrides: Partial<ChannelRoutingMetrics> = {}
): ChannelRoutingMetrics {
  return {
    id: 1,
    name: 'ch',
    supplier: 'OpenAI',
    lab_group_slug: 'openai',
    lab_group_name: 'OpenAI',
    latency: 300,
    quota: 100,
    weight: 10,
    priority: 1,
    status: 1,
    ...overrides,
  }
}

const channels: RouteChannelRow[] = scoreChannels([
  makeChannel({ id: 1, latency: 900, quota: 20 }),
  makeChannel({ id: 2, latency: 120, quota: 800 }),
  makeChannel({ id: 3, latency: 400, quota: 200 }),
]).map((channel, index) => ({ ...channel, rank: index + 1 }))

const monitor: RouteHealthSummary = {
  checks: Array.from({ length: 6 }, (_, index) => ({
    timestamp: 1_800 + index * 600,
    state: index === 2 ? ('degraded' as const) : ('healthy' as const),
  })),
  state: 'healthy',
  availability: 100,
}

function render(
  overrides: {
    groupSlug?: string
    groupName?: string
    channels?: RouteChannelRow[]
    activeCount?: number
    monitor?: RouteHealthSummary
  } = {}
) {
  return mount(VendorRouteGroup, {
    props: {
      groupSlug: overrides.groupSlug ?? 'openai',
      groupName: overrides.groupName ?? 'OpenAI',
      labMatches: [],
      channels: overrides.channels ?? channels,
      activeCount: overrides.activeCount ?? channels.length,
      monitor: overrides.monitor ?? monitor,
    },
    global: { plugins: [i18n] },
    // jsdom only refreshes computed style (and so isVisible) when attached.
    attachTo: document.body,
  })
}

describe('VendorRouteGroup', () => {
  it('keeps the canonical lab slug logo mapping aligned with assets', () => {
    const expected = {
      openai: 'OpenAI',
      anthropic: 'Anthropic',
      google: 'Google',
      deepseek: 'DeepSeek',
      alibaba: 'Alibaba',
      xai: 'xAI',
      moonshotai: 'Moonshot AI',
      zhipuai: 'Zhipu AI',
      minimax: 'MiniMax',
      mistral: 'Mistral',
      tencent: 'Tencent',
      'bytedance-seed': 'Bytedance Seed',
    }

    expect(labLogoVendor).toEqual(expected)
    for (const vendor of Object.values(expected)) {
      expect(vendorLogoMeta[vendor]).toBeDefined()
    }
  })

  it('normalizes lab slugs before selecting a logo', () => {
    const wrapper = render({ groupSlug: '  OPENAI  ' })

    expect(wrapper.find('[data-route-lab-logo] img').attributes('src')).toBe(
      '/models/openai.svg'
    )
  })

  it('shows the mapped lab logo and keeps channel rows text-only', async () => {
    const wrapper = render()

    expect(wrapper.find('[data-route-lab-logo] img').attributes('src')).toBe(
      '/models/openai.svg'
    )
    expect(wrapper.find('[data-route-lab-fallback]').exists()).toBe(false)

    await wrapper.find('button[aria-expanded]').trigger('click')
    expect(wrapper.findAll('[data-route-channel] img')).toHaveLength(0)
    expect(wrapper.find('[data-route-channel]').text()).toContain('OpenAI')
  })

  it.each([
    ['mixed', 'Mixed / Multi-Lab'],
    ['unknown', 'Unknown / Provider-specific'],
    ['custom-lab', 'Custom Lab'],
  ])('uses the neutral icon for %s lab groups', (groupSlug, groupName) => {
    const wrapper = render({ groupSlug, groupName })

    expect(wrapper.find('[data-route-lab-logo]').exists()).toBe(false)
    expect(wrapper.find('[data-route-lab-fallback]').exists()).toBe(true)
  })

  it('shows six monitoring buckets and the group availability', () => {
    const wrapper = render()

    expect(wrapper.attributes('data-route-vendor')).toBe('')
    expect(wrapper.attributes('data-route-group')).toBe('')
    expect(wrapper.text()).toContain('OpenAI')
    expect(
      wrapper.find('button[aria-expanded]').attributes('aria-expanded')
    ).toBe('false')
    expect(wrapper.findAll('[data-route-health-cell]')).toHaveLength(6)
    expect(wrapper.text()).toContain('1h 100.00%')
    expect(wrapper.text()).toContain('Operational')
  })

  it('shows the best score and crowns exactly one channel after expansion', async () => {
    const wrapper = render()

    expect(wrapper.find('button[aria-expanded]').text()).toContain(
      String(channels[0]!.score)
    )
    expect(wrapper.findAll('[title="Group best"]')).toHaveLength(0)

    await wrapper.find('button[aria-expanded]').trigger('click')
    expect(wrapper.findAll('[title="Leading operating score"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-route-channel]')).toHaveLength(
      channels.length
    )
  })

  it('keeps the monitor visible while channel details are expanded', async () => {
    const wrapper = render()
    expect(wrapper.findAll('[title*="≈"]')).toHaveLength(0)
    expect(wrapper.findAll('[data-route-health-cell]')).toHaveLength(6)

    await wrapper.find('button[aria-expanded]').trigger('click')

    expect(
      wrapper.find('button[aria-expanded]').attributes('aria-expanded')
    ).toBe('true')
    expect(wrapper.findAll('[title*="≈"]')).toHaveLength(12)
    expect(wrapper.findAll('[data-route-health-cell]')).toHaveLength(6)
  })

  it('retains an unavailable group and labels disabled channels without scores', async () => {
    const inactive: RouteChannelRow = {
      ...makeChannel({ status: 3 }),
      rank: null,
      score: null,
      breakdown: null,
    }
    const wrapper = render({
      channels: [inactive],
      activeCount: 0,
      monitor: {
        checks: monitor.checks.map((check) => ({ ...check, state: 'down' })),
        state: 'down',
        availability: 0,
      },
    })

    expect(wrapper.text()).toContain('0/1 available')
    expect(wrapper.text()).toContain('Outage')
    expect(wrapper.text()).toContain('1h 0.00%')
    expect(wrapper.text()).not.toContain('Auto-disabled')
    expect(wrapper.find('[aria-label^="Score"]').exists()).toBe(false)

    await wrapper.find('button[aria-expanded]').trigger('click')
    expect(wrapper.text()).toContain('Auto-disabled')
  })

  it('renders at most 3 top channels when long, and reveals remaining channels on toggle', async () => {
    const scoredFive: RouteChannelRow[] = [
      ...scoreChannels([
        makeChannel({ id: 10, latency: 100, quota: 900 }),
        makeChannel({ id: 20, latency: 200, quota: 800 }),
        makeChannel({ id: 30, latency: 300, quota: 700 }),
        makeChannel({ id: 40, latency: 400, quota: 600 }),
      ]).map((ch, idx) => ({ ...ch, rank: idx + 1 })),
      {
        ...makeChannel({ id: 50, status: 2 }),
        rank: null,
        score: null,
        breakdown: null,
      },
    ]

    const wrapper = render({
      channels: scoredFive,
      activeCount: 4,
    })

    // Expand group
    await wrapper.find('button[aria-expanded]').trigger('click')

    // Only top 3 channels are shown by default
    expect(visibleChannelNames(wrapper)).toHaveLength(3)
    const toggle = wrapper.find('[data-route-more-toggle]')
    expect(toggle.exists()).toBe(true)
    expect(toggle.text()).toBe('Show 2 more channels')

    // Click toggle to reveal the extra 2 channels
    await toggle.trigger('click')
    expect(visibleChannelNames(wrapper)).toHaveLength(5)
    // The extra channels should be rendered in compact mode
    expect(wrapper.findAll('[data-route-channel-compact]')).toHaveLength(2)
    expect(toggle.text()).toBe('Hide extra channels')

    // Click toggle again to collapse
    await toggle.trigger('click')
    expect(visibleChannelNames(wrapper)).toHaveLength(3)
  })

  it('does not display more-toggle button when channel count is <= 3', async () => {
    const wrapper = render()
    await wrapper.find('button[aria-expanded]').trigger('click')

    expect(wrapper.findAll('[data-route-channel]')).toHaveLength(3)
    expect(wrapper.find('[data-route-more-toggle]').exists()).toBe(false)
  })

  it('still shows three rows when every channel in the group is inactive', async () => {
    const allInactive = [1, 2, 3, 4, 5].map((id) =>
      inactiveRow({ id, name: `down-${id}` })
    )
    const wrapper = render({ channels: allInactive, activeCount: 0 })
    await wrapper.find('button[aria-expanded]').trigger('click')

    expect(visibleChannelNames(wrapper)).toEqual(['down-1', 'down-2', 'down-3'])
    expect(wrapper.find('[data-route-more-toggle]').text()).toBe(
      'Show 2 more channels'
    )
  })

  it('fills the collapsed view with inactive rows after the ranked ones', async () => {
    const mixed = [
      ...scoreChannels([
        makeChannel({ id: 1, name: 'fast', latency: 100, quota: 900 }),
        makeChannel({ id: 2, name: 'slow', latency: 900, quota: 100 }),
      ]).map((channel, index) => ({ ...channel, rank: index + 1 })),
      inactiveRow({ id: 3, name: 'off-a' }),
      inactiveRow({ id: 4, name: 'off-b' }),
    ]
    const wrapper = render({ channels: mixed, activeCount: 2 })
    await wrapper.find('button[aria-expanded]').trigger('click')

    // Upstream order is kept as-is: ranked first, then inactive.
    expect(visibleChannelNames(wrapper)).toEqual(['fast', 'slow', 'off-a'])
    expect(wrapper.find('[data-route-more-toggle]').text()).toBe(
      'Show 1 more channel'
    )
  })

  it('links the more toggle to the rows it reveals', async () => {
    const wrapper = render({
      channels: [
        ...channels,
        inactiveRow({ id: 4, name: 'off-a' }),
        inactiveRow({ id: 5, name: 'off-b' }),
      ],
    })
    await wrapper.find('button[aria-expanded]').trigger('click')

    const toggle = wrapper.find('[data-route-more-toggle]')
    const controlsId = toggle.attributes('aria-controls')
    expect(controlsId).toBeTruthy()
    const controlled = wrapper.find(`[id="${controlsId}"]`)
    expect(controlled.exists()).toBe(true)
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(controlled.isVisible()).toBe(false)

    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(controlled.isVisible()).toBe(true)
    expect(controlled.findAll('[data-route-channel]')).toHaveLength(2)
  })
})

function inactiveRow(
  overrides: Partial<ChannelRoutingMetrics> = {}
): RouteChannelRow {
  return {
    ...makeChannel({ status: 2, ...overrides }),
    rank: null,
    score: null,
    breakdown: null,
  }
}

/** Names of the channel rows a user can currently see, in display order. */
function visibleChannelNames(wrapper: ReturnType<typeof render>): string[] {
  return wrapper
    .findAll('[data-route-channel]')
    .filter((row) => row.isVisible())
    .map((row) => row.find('p[title]').text())
}
