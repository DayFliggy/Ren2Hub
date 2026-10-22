import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'

import ChannelMobileList from '@/components/console/channels/ChannelMobileList.vue'
import type { AdminChannel } from '@/types/console'

const base: AdminChannel = {
  id: 1,
  name: 'openai',
  type: 1,
  channel_ratio: '1',
  max_concurrency: 0,
  in_flight: 0,
  supplier: 'OpenAI',
  lab: { id: 'openai', name: 'OpenAI', source: 'default' },
  status: 1,
  priority: 0,
  weight: 0,
  used_quota: 0,
  balance: 0,
  response_time: 0,
  test_time: 0,
  base_url: '',
  models: 'gpt-4o',
  model_mapping: '',
}

function channel(patch: Partial<AdminChannel>): AdminChannel {
  return { ...base, ...patch }
}

function mountList(channels: AdminChannel[]) {
  const refreshBalance = vi.fn().mockResolvedValue(true)
  const runSupplierBalance = vi.fn().mockResolvedValue(undefined)
  const wrapper = mount(ChannelMobileList, {
    props: {
      groups: [{ groupSlug: 'g', supplier: 'Group', channels, labMatches: [] }],
      visibleFields: ['upstream', 'rowUpstreamAction'],
      selectedIds: [],
      allSelected: false,
      selectionDisabled: false,
      toggleAllSelected: vi.fn(),
      toggleSelected: vi.fn(),
      isSupplierCollapsed: () => false,
      toggleSupplier: vi.fn(),
      batchProgress: null,
      groupTestOverrides: {},
      canRunBatch: true,
      canMutate: true,
      canOperate: true,
      canWrite: true,
      canSensitiveWrite: true,
      runSupplierBalance,
      pickSupplierTest: vi.fn(),
      clearSupplier: vi.fn(),
      isBusy: () => false,
      isRowBusy: () => false,
      updateNumber: vi.fn(),
      toggleStatus: vi.fn(),
      refreshBalance,
      openTest: vi.fn(),
      editChannel: vi.fn(),
      syncModels: vi.fn(),
      showRuntime: vi.fn(),
      deleteChannel: vi.fn(),
    },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
    },
  })
  return { wrapper, refreshBalance, runSupplierBalance }
}

function balanceButtons(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll('button')
    .filter((button) =>
      button.attributes('aria-label')?.includes('channels.refreshBalance')
    )
}

function groupBalanceButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll('button')
    .find((button) =>
      button.attributes('aria-label')?.includes('channels.syncSupplier')
    )
}

describe('channel mobile list balance actions', () => {
  it('offers balance refresh only where the backend has a balance branch', async () => {
    const { wrapper, refreshBalance } = mountList([
      channel({ id: 1, type: 1 }),
      channel({ id: 2, type: 3 }),
      channel({ id: 3, type: 61 }),
      channel({
        id: 4,
        type: 1,
        channel_info: { is_multi_key: true, multi_key_mode: 'random' },
      }),
      channel({ id: 5, type: 58 }),
    ])

    const buttons = balanceButtons(wrapper)
    expect(buttons).toHaveLength(2)
    for (const button of buttons) await button.trigger('click')
    expect(refreshBalance.mock.calls.map(([item]) => item.id)).toEqual([1, 5])
    expect(groupBalanceButton(wrapper)).toBeDefined()
  })

  it('hides the group balance action when no channel supports it', () => {
    const { wrapper } = mountList([
      channel({ id: 2, type: 14 }),
      channel({ id: 3, type: 61 }),
    ])

    expect(balanceButtons(wrapper)).toHaveLength(0)
    expect(groupBalanceButton(wrapper)).toBeUndefined()
  })
})
