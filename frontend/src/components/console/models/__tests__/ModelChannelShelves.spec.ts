import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n, { setLocale } from '@/i18n'
import ModelChannelShelves from '../ModelChannelShelves.vue'
import type {
  MarketChannelShelf,
  MarketModel,
  MarketOffer,
} from '@/types/modelMarket'

function sampleShelf(): MarketChannelShelf {
  const pricing = {
    id: 'gpt-4o:11',
    billingMode: 'token' as const,
    priceLines: [
      { key: 'input', amount: '2.5', unit: 'per_million_tokens' as const },
      { key: 'output', amount: '10', unit: 'per_million_tokens' as const },
    ],
    endpointTypes: ['openai'],
  }
  const offer1: MarketOffer = {
    id: 'gpt-4o:11',
    modelId: 'gpt-4o',
    channelId: 11,
    channelName: 'Primary Pool',
    ratio: '1',
    pricing,
    baseline: { ...pricing, id: 'gpt-4o' },
    endpointTypes: ['openai'],
    health: { availability: 'paused', quality: 'unrated' },
  }
  const offer2: MarketOffer = {
    ...offer1,
    id: 'claude-sonnet-4-5:11',
    modelId: 'claude-sonnet-4-5',
    health: { availability: 'no_restriction', quality: 'unrated' },
  }
  const model1: MarketModel = {
    id: 'gpt-4o',
    name: 'gpt-4o',
    vendor: 'OpenAI',
    icon: '',
    description: '',
    tags: [],
    type: 'chat',
    endpointTypes: ['openai'],
    endpoints: [],
    baselines: [offer1.baseline],
    offers: [offer1],
  }
  const model2: MarketModel = {
    ...model1,
    id: 'claude-sonnet-4-5',
    name: 'claude-sonnet-4-5',
    offers: [offer2],
  }
  return {
    channel: {
      id: 11,
      name: 'Primary Pool',
      ratio: '1',
      health: {
        visible_models: 2,
        affected_models: 1,
        evaluated_models: 0,
        unrated_models: 1,
      },
    },
    items: [
      { model: model1, offers: [offer1] },
      { model: model2, offers: [offer2] },
    ],
  }
}

describe('ModelChannelShelves', () => {
  beforeEach(async () => {
    await setLocale('zh-CN')
  })

  it('renders channel-level health summary in header and per-offer health badges without platform telemetry', () => {
    const wrapper = mount(ModelChannelShelves, {
      props: { shelves: [sampleShelf()] },
      global: { plugins: [i18n] },
    })

    // Platform-wide perf_metrics telemetry must never appear in channel shelves.
    expect(wrapper.find('[data-market-telemetry]').exists()).toBe(false)

    // Channel header summarizes visible model health counts.
    const channelSummary = wrapper.get('[data-catalog-channel-health]')
    expect(channelSummary.text()).toContain('1 个模型受影响')
    expect(channelSummary.text()).toContain('1 个待评估')

    // Each offer renders its own health state and connects via aria-describedby.
    const offerBadges = wrapper.findAll('[data-catalog-offer-health]')
    expect(offerBadges).toHaveLength(2)
    expect(offerBadges[0]!.attributes('data-catalog-offer-health')).toBe(
      'paused'
    )
    expect(offerBadges[0]!.text()).toContain('自动路由暂停')
    expect(offerBadges[1]!.attributes('data-catalog-offer-health')).toBe(
      'unrated'
    )
    expect(offerBadges[1]!.text()).toContain('近期样本不足')

    const hitButton = wrapper.get('button[data-offer-id="gpt-4o:11"]')
    const healthId = offerBadges[0]!.attributes('id')
    expect(healthId).toBeTruthy()
    expect(hitButton.attributes('aria-describedby')).toContain(healthId)
    expect(wrapper.find('[data-catalog-channel-scope]').exists()).toBe(false)
  })

  it('shows a visible scope note when client filters show fewer models than the channel summary', () => {
    const shelf = sampleShelf()
    shelf.items = shelf.items.slice(0, 1)
    const wrapper = mount(ModelChannelShelves, {
      props: { shelves: [shelf] },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('[data-catalog-channel-scope]').text()).toContain(
      '按该渠道全部 2 个模型'
    )
  })
})
