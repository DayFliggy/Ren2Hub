import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import KeyMobileList from '@/components/console/keys/KeyMobileList.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { TokenSummary } from '@/types/console'

const token: TokenSummary = {
  id: 7,
  name: 'Production key',
  key_preview: 'sk-prod...1234',
  type: 'auto',
  selected_only: false,
  route_channel_count: 0,
  status: 1,
  used_quota: '500000',
  remain_quota: 1_500_000,
  unlimited: false,
  model_limits: [],
  ip_limits: [],
  expired_time: -1,
  created_time: 1_722_000_000,
}

async function mountList(tokens: TokenSummary[] = [token]) {
  await loadMessageDomain('console')
  await setLocale('zh-CN')

  const actions = {
    toggleAllSelected: vi.fn(),
    toggleSelected: vi.fn(),
    toggleStatus: vi.fn(),
    viewKey: vi.fn(),
    editKey: vi.fn(),
    deleteKey: vi.fn(),
  }
  const wrapper = mount(KeyMobileList, {
    props: {
      tokens,
      selectedIds: [],
      allSelected: false,
      isToggling: () => false,
      now: 1_800_000_000,
      ...actions,
    },
    global: { plugins: [i18n] },
  })

  return { wrapper, actions }
}

describe('KeyMobileList', () => {
  it('renders the complete mobile token summary without a wide table', async () => {
    const { wrapper } = await mountList()

    expect(wrapper.get('[data-key-mobile-row]').text()).toContain(
      'Production key'
    )
    expect(wrapper.text()).toContain('sk-prod...1234')
    expect(wrapper.get('[data-token-type]').text()).toBe('自动 · 全平台')
    expect(wrapper.text()).toContain('永不过期')
  })

  it('names the routing scope, paused fallback, and orphaned selection warning', async () => {
    const configureRouting = vi.fn()
    await loadMessageDomain('console')
    await setLocale('zh-CN')
    const wrapper = mount(KeyMobileList, {
      props: {
        tokens: [
          {
            ...token,
            id: 1,
            type: 'ordered',
            selected_only: true,
            route_channel_count: 1,
            enabled_route_channel_count: 1,
            route_scope: 'selected',
            effective_mode: 'ordered',
          },
          {
            ...token,
            id: 2,
            type: 'auto',
            selected_only: true,
            route_channel_count: 4,
            enabled_route_channel_count: 4,
            route_scope: 'selected',
            effective_mode: 'auto',
          },
          {
            ...token,
            id: 3,
            type: 'ordered',
            selected_only: true,
            route_channel_count: 2,
            enabled_route_channel_count: 0,
            route_scope: 'platform',
            effective_mode: 'auto',
          },
          {
            ...token,
            id: 4,
            type: 'auto',
            selected_only: true,
            route_channel_count: 0,
            enabled_route_channel_count: 0,
            route_scope: 'empty',
            effective_mode: 'auto',
          },
        ],
        selectedIds: [],
        allSelected: false,
        isToggling: () => false,
        toggleAllSelected: vi.fn(),
        toggleSelected: vi.fn(),
        toggleStatus: vi.fn(),
        viewKey: vi.fn(),
        editKey: vi.fn(),
        configureRouting,
        deleteKey: vi.fn(),
        routingEnabled: true,
        now: 1_800_000_000,
      },
      global: { plugins: [i18n] },
    })
    const types = wrapper.findAll('[data-token-type]')
    expect(types.map((item) => item.text())).toEqual([
      '顺序 · 1 条渠道',
      '自动 · 4 条渠道',
      '自动 · 全平台 · 2 条已暂停',
      expect.stringContaining('自选渠道已失效'),
    ])
    expect(types[3]!.find('[data-route-warning]').exists()).toBe(true)
    expect(types[0]!.find('[data-route-warning]').exists()).toBe(false)
    await types[3]!.get('[data-key-route-trigger]').trigger('click')
    expect(configureRouting).toHaveBeenCalledWith(
      expect.objectContaining({ id: 4 })
    )

    const routingButtons = wrapper.findAll('[data-key-routing-action]')
    expect(routingButtons).toHaveLength(4)
    await routingButtons[0]!.trigger('click')
    expect(configureRouting).toHaveBeenLastCalledWith(
      expect.objectContaining({ id: 1 })
    )
  })

  it('says why a key is blocked next to its ring, not by colour alone', async () => {
    const { wrapper } = await mountList([
      { ...token, status: 2 },
      { ...token, id: 8, status: 4, remain_quota: 0 },
    ])
    const [disabled, exhausted] = wrapper.findAll('[data-key-mobile-row]')

    expect(
      disabled!.get('[data-ring-state]').attributes('data-ring-state')
    ).toBe('disabled')
    expect(disabled!.get('[data-key-quota-note]').text()).toBe('禁用')
    expect(disabled!.find('[data-ring-state] path').exists()).toBe(true)
    expect(
      exhausted!.get('[data-ring-state]').attributes('data-ring-state')
    ).toBe('exhausted')
    expect(exhausted!.get('[data-key-quota-note]').text()).toBe('额度耗尽')
    // A spent balance is the one shape no mark is needed for: a full red ring.
    expect(exhausted!.find('[data-ring-state] path').exists()).toBe(false)
  })

  it('renders the quota bar as a static readout without an accordion button', async () => {
    const { wrapper } = await mountList([
      {
        ...token,
        model_limits: ['gpt-4o', 'claude-4'],
        ip_limits: ['10.0.0.1'],
        allow_user_quota_query: false,
      },
      { ...token, id: 8 },
    ])
    const [first] = wrapper.findAll('[data-key-mobile-row]')
    const bar = first!.get('.key-quota-bar')

    expect(bar.get('[data-key-remaining]').text()).toBe('$3.000')
    expect(bar.text()).toContain('已用 $1.000')
    expect(first!.find('button[data-key-quota-ring]').exists()).toBe(false)
    expect(first!.find('[aria-expanded]').exists()).toBe(false)
    expect(first!.get('[data-key-restrictions]').text()).toContain(
      '限 2 个模型'
    )
    expect(first!.get('[data-key-restrictions]').text()).toContain('限 IP')
    expect(first!.get('[data-key-restrictions]').text()).toContain('禁查额度')
  })

  it('tints the quota bar by urgency, never for a blocked key', async () => {
    const { wrapper } = await mountList([
      { ...token, remain_quota: 50_000, used_quota: '950000' },
      { ...token, id: 8, remain_quota: 0 },
      { ...token, id: 9, status: 2, remain_quota: 0 },
    ])
    expect(
      wrapper
        .findAll('.key-quota-bar')
        .map((bar) => bar.attributes('data-tone'))
    ).toEqual(['warning', 'danger', 'quiet'])
  })

  it('fades only the cards whose key cannot serve', async () => {
    const { wrapper } = await mountList([
      { ...token, status: 2 },
      { ...token, id: 8, status: 4, remain_quota: 0 },
      // Past its expiry before the backend has flipped the status.
      { ...token, id: 9, expired_time: 1_700_000_000 },
      { ...token, id: 10 },
    ])
    expect(
      wrapper
        .findAll('[data-key-mobile-row]')
        .map((card) => card.classes().includes('opacity-80'))
    ).toEqual([true, false, true, false])
  })

  it('keeps selection and every row action reachable', async () => {
    const { wrapper, actions } = await mountList()
    const row = wrapper.get('[data-key-mobile-row]')

    await wrapper.get('label input[type="checkbox"]').trigger('change')
    await row.get('input[type="checkbox"]').trigger('change')
    await row.get('footer button[aria-label="启停"]').trigger('click')
    await row.get('button[aria-label="查看完整密钥"]').trigger('click')
    await row.get('button[aria-label="编辑令牌"]').trigger('click')
    await row.get('button[aria-label="删除"]').trigger('click')

    expect(actions.toggleAllSelected).toHaveBeenCalledOnce()
    expect(actions.toggleSelected).toHaveBeenCalledWith(token)
    expect(actions.toggleStatus).toHaveBeenCalledWith(token)
    expect(actions.viewKey).toHaveBeenCalledWith(token)
    expect(actions.editKey).toHaveBeenCalledWith(token)
    expect(actions.deleteKey).toHaveBeenCalledWith(token)
    expect(row.find('[data-key-routing-action]').exists()).toBe(false)
  })
})
