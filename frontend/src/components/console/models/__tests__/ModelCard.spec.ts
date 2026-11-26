import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'
import ModelCard from '../ModelCard.vue'
import ModelPriceDisplay from '../ModelPriceDisplay.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { buildModelMarketCatalog } from '@/utils/modelMarket'
import { modelCatalogFixture } from '@/api/__tests__/modelCatalogFixture'
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})
const noSamples = { status: 'empty' } as const
describe('native catalog cards', () => {
  it('omits platform baseline on cards and renders neutral placeholder telemetry when unsampled', async () => {
    const model = buildModelMarketCatalog(modelCatalogFixture()).models[0]!
    const wrapper = mount(ModelCard, {
      props: { model, performance: noSamples },
      global: { plugins: [i18n] },
    })
    expect(wrapper.find('[data-market-baseline]').exists()).toBe(false)
    expect(wrapper.get('[data-channel-quote]').text()).toContain('$0.250')
    expect(wrapper.get('[data-channel-quote]').text()).toContain(
      'Fixture channel'
    )
    // Model-wide metrics never sit inside the channel lane they do not describe.
    expect(
      wrapper.find('[data-channel-quote] [data-market-telemetry]').exists()
    ).toBe(false)
    const placeholder = wrapper.get('[data-model-statistics="empty"]')
    expect(placeholder.attributes('aria-label')).toContain('近 24 小时')
    expect(placeholder.text()).toContain('--')
    expect(wrapper.find('[data-model-statistics-unavailable]').exists()).toBe(
      false
    )
    expect(wrapper.text()).not.toContain('近 24 小时')
    expect(wrapper.text()).not.toMatch(/\d+%|优惠|最低|最优/)
    await wrapper.get('button[aria-label*="模型详情"]').trigger('click')
    expect(wrapper.emitted('detail')).toEqual([
      ['fixture-model', 'fixture-model:9'],
    ])
    wrapper.unmount()
  })
  it('shows unavailable prices without fake zero prices or capability placeholders', () => {
    const fixture = modelCatalogFixture()
    fixture.models[0]!.price_status = 'unavailable'
    fixture.models[0]!.pricing_values = {}
    fixture.offers[0]!.price_status = 'unavailable'
    fixture.offers[0]!.pricing_values = {}
    const model = buildModelMarketCatalog(fixture).models[0]!
    const wrapper = mount(ModelCard, {
      props: { model, performance: noSamples },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('价格不可静态确定')
    expect(wrapper.text()).not.toContain('$0.000')
    expect(wrapper.text()).not.toMatch(/未提供|上下文|工具调用/)
    wrapper.unmount()
  })
  it('renders cache, image and audio dimensions without dropping their price', () => {
    const fixture = modelCatalogFixture()
    fixture.models[0]!.pricing_values = {
      input: '1',
      cache: '0.25',
      cache_create: '1.25',
      cache_create_1h: '2',
      cache_create_12h: '2.5',
      image: '3',
      image_cache: '0.5',
      image_output: '4',
      audio_input: '5',
      audio_output: '10',
    }
    const model = buildModelMarketCatalog(fixture).models[0]!
    const wrapper = mount(ModelPriceDisplay, {
      props: { pricing: model.baselines[0], mode: 'full' },
      global: { plugins: [i18n] },
    })
    for (const dimension of [
      'cacheRead',
      'cacheWrite',
      'cacheWrite1h',
      'cacheWrite12h',
      'image',
      'imageCache',
      'imageOutput',
      'audioInput',
      'audioOutput',
    ]) {
      const line = wrapper.get(`[data-price-dimension="${dimension}"]`)
      expect(line.text()).toContain('$')
      expect(line.text()).not.toContain(dimension)
    }
    wrapper.unmount()
  })
  it.each(['expression_requires_usage_context', 'private_internal_error'])(
    'uses public wording for unavailable reason %s',
    (reason) => {
      const fixture = modelCatalogFixture()
      fixture.offers[0]!.price_status = 'unavailable'
      fixture.offers[0]!.pricing_values = {}
      fixture.offers[0]!.price_reason = reason
      const model = buildModelMarketCatalog(fixture).models[0]!
      const wrapper = mount(ModelPriceDisplay, {
        props: { pricing: model.offers[0]!.pricing },
        global: { plugins: [i18n] },
      })
      expect(wrapper.text()).not.toContain(reason)
      expect(wrapper.text()).not.toContain('$0')
      expect(wrapper.text()).toMatch(/价格|用量/)
      wrapper.unmount()
    }
  )
  it.each([
    ['zh-CN', 'invalid_audio_price', '音频价格超出可显示范围'],
    ['zh-CN', 'protocol_dependent_audio_price', '音频价格因调用协议而异'],
    [
      'en',
      'invalid_audio_price',
      'The audio price exceeds the displayable range',
    ],
    [
      'en',
      'protocol_dependent_audio_price',
      'Audio prices differ by request protocol',
    ],
  ] as const)(
    'localizes specialized unavailable reason %s / %s',
    async (locale, reason, message) => {
      await setLocale(locale)
      const fixture = modelCatalogFixture()
      fixture.offers[0]!.price_status = 'unavailable'
      fixture.offers[0]!.pricing_values = {}
      fixture.offers[0]!.price_reason = reason
      const projected =
        buildModelMarketCatalog(fixture).models[0]!.offers[0]!.pricing
      const wrapper = mount(ModelPriceDisplay, {
        props: { pricing: projected },
        global: { plugins: [i18n] },
      })
      expect(wrapper.text()).toContain(message)
      expect(wrapper.text()).not.toContain(reason)
      expect(wrapper.text()).not.toContain('$0')
      wrapper.unmount()
      await setLocale('zh-CN')
    }
  )
  it('retains an explicitly zero price and omits unsupported billing dimensions', () => {
    const fixture = modelCatalogFixture()
    fixture.models[0]!.price_status = 'zero'
    fixture.models[0]!.pricing_values = { input: '0', output: '0' }
    const model = buildModelMarketCatalog(fixture).models[0]!
    const wrapper = mount(ModelPriceDisplay, {
      props: { pricing: model.baselines[0], mode: 'full' },
      global: { plugins: [i18n] },
    })
    expect(wrapper.get('[data-price-dimension="input"]').text()).toContain(
      '$0.000'
    )
    expect(wrapper.find('[data-price-dimension="cacheRead"]').exists()).toBe(
      false
    )
    wrapper.unmount()
  })
  it('omits routine configured status while keeping default and zero-price states', () => {
    const fixture = modelCatalogFixture()
    const pricing = buildModelMarketCatalog(fixture).models[0]!.baselines[0]!
    const configured = mount(ModelPriceDisplay, {
      props: { pricing: { ...pricing, priceStatus: 'configured' } },
      global: { plugins: [i18n] },
    })
    expect(configured.text()).toContain('$1.250')
    expect(configured.text()).not.toContain('已配置价格')
    configured.unmount()

    for (const [priceStatus, label] of [
      ['default', '默认价格'],
      ['zero', '零价格'],
    ] as const) {
      const wrapper = mount(ModelPriceDisplay, {
        props: { pricing: { ...pricing, priceStatus } },
        global: { plugins: [i18n] },
      })
      expect(wrapper.text()).toContain(label)
      wrapper.unmount()
    }
  })
  it('renders ready model statistics and distinct compact placeholder states for empty/loading/error', () => {
    const model = buildModelMarketCatalog(modelCatalogFixture()).models[0]!
    const wrapper = mount(ModelCard, {
      props: {
        model,
        performance: {
          status: 'ready',
          summary: {
            model_name: model.name,
            success_rate: 99.5,
            avg_latency_ms: 80,
            avg_tps: 10.5,
          },
        },
      },
      global: { plugins: [i18n] },
    })
    const statistics = wrapper.get('[data-model-statistics="ready"]')
    expect(statistics.text()).toContain('99.5%')
    expect(statistics.text()).toContain('10.5')
    expect(statistics.attributes('aria-label')).toContain('近 24 小时')
    expect(statistics.text()).not.toContain('近 24 小时')
    wrapper.unmount()

    for (const status of ['loading', 'error'] as const) {
      const stateWrapper = mount(ModelCard, {
        props: { model, performance: { status } },
        global: { plugins: [i18n] },
      })
      const bar = stateWrapper.get(`[data-model-statistics="${status}"]`)
      expect(bar.text()).not.toMatch(/\d+%/)
      stateWrapper.unmount()
    }
  })
  it('keeps >3 channels on a single rail with an overflow popover that switches preview', async () => {
    const fixture = modelCatalogFixture()
    fixture.models[0]!.tags = '推理,视觉,代码,长上下文'
    for (let id = 10; id <= 13; id++) {
      fixture.channels.push({
        ...fixture.channels[0]!,
        id,
        name: `Channel-${id}`,
        channel_ratio: `0.${id - 7}`,
      })
      fixture.offers.push({
        ...fixture.offers[0]!,
        channel_id: id,
        channel_ratio: `0.${id - 7}`,
        pricing_values: { input: `${id}.0`, output: `${id * 2}.0` },
      })
    }
    const model = buildModelMarketCatalog(fixture).models[0]!
    const wrapper = mount(ModelCard, {
      props: { model, performance: noSamples },
      global: { plugins: [i18n] },
    })
    // Real tags are capped at 3 pills + overflow count.
    const tags = wrapper.get('[data-model-tags]').text()
    expect(tags).toContain('推理')
    expect(tags).toContain('视觉')
    expect(tags).toContain('代码')
    expect(tags).toContain('+1')
    expect(tags).not.toContain('长上下文')

    // 5 total channels -> 3 inline chips + "+2" overflow trigger.
    expect(wrapper.findAll('[data-channel-chip]')).toHaveLength(3)
    const overflow = wrapper.get('[data-channel-overflow]')
    expect(overflow.text()).toBe('+2')
    await overflow.trigger('click')
    const menu = wrapper.get('[data-channel-overflow-menu]')
    expect(menu.findAll('[data-channel-overflow-item]')).toHaveLength(2)

    // Selecting a hidden channel updates the inline quote stage and detail offerId.
    await menu.get('[data-channel-overflow-item="13"]').trigger('click')
    expect(wrapper.find('[data-channel-overflow-menu]').exists()).toBe(false)
    expect(wrapper.get('[data-channel-quote]').text()).toContain('Channel-13')
    expect(wrapper.get('[data-channel-quote]').text()).toContain('$13.000')
    await wrapper.get('button[aria-label*="模型详情"]').trigger('click')
    expect(wrapper.emitted('detail')).toEqual([
      ['fixture-model', 'fixture-model:13'],
    ])
    wrapper.unmount()
  })
  it('places compact status badge inline beside billing, shows full status toast on channel hover, and cycles channels only when multiple channels exist', async () => {
    // Single-channel model must not render the channel-cycle refresh button.
    const singleModel = buildModelMarketCatalog(modelCatalogFixture())
      .models[0]!
    const singleWrapper = mount(ModelCard, {
      attachTo: document.body,
      props: { model: singleModel, performance: noSamples },
      global: { plugins: [i18n] },
    })
    expect(singleWrapper.find('[data-cycle-channel]').exists()).toBe(false)
    const singleBadge = singleWrapper.get(
      '.market-quote-heading .market-quote-meta [data-catalog-offer-health]'
    )
    expect(singleBadge.text()).toBe('暂无')
    singleWrapper.unmount()

    // Multi-channel model renders badge inline beside billing, hover toast, and cycle button.
    const fixture = modelCatalogFixture()
    fixture.offers[0]!.health = {
      availability: 'no_restriction',
      quality: 'no_downgrade',
    }
    fixture.channels.push(
      {
        ...fixture.channels[0]!,
        id: 10,
        name: '限流通道',
        channel_ratio: '0.5',
      },
      {
        ...fixture.channels[0]!,
        id: 11,
        name: '恢复通道',
        channel_ratio: '0.8',
      }
    )
    fixture.offers.push(
      {
        ...fixture.offers[0]!,
        channel_id: 10,
        channel_ratio: '0.5',
        pricing_values: { input: '1.25', output: '5.0' },
        health: { availability: 'limited', quality: 'degraded' },
      },
      {
        ...fixture.offers[0]!,
        channel_id: 11,
        channel_ratio: '0.8',
        pricing_values: { input: '2.0', output: '8.0' },
        health: { availability: 'recovering', quality: 'unrated' },
      }
    )
    const multiModel = buildModelMarketCatalog(fixture).models[0]!
    const wrapper = mount(ModelCard, {
      attachTo: document.body,
      props: { model: multiModel, performance: noSamples },
      global: { plugins: [i18n] },
    })

    // Badge sits inside .market-quote-meta right after .market-billing-label.
    const quoteMeta = wrapper.get('.market-quote-heading .market-quote-meta')
    expect(quoteMeta.text()).toContain('按量')
    expect(
      quoteMeta.get('[data-catalog-offer-health="no_downgrade"]').text()
    ).toBe('正常')

    // Hovering a channel chip renders a teleported mini-toast with full status & secondary detail.
    const secondChip = wrapper.get('[data-channel-chip="10"]')
    await secondChip.trigger('mouseenter')
    const toast = document.body.querySelector<HTMLElement>(
      '[data-channel-health-toast]'
    )!
    expect(toast).not.toBeNull()
    expect(toast.textContent).toContain('限流通道')
    expect(toast.textContent).toContain('渠道临时受限')
    expect(toast.textContent).toContain('部分调用质量下降')
    await secondChip.trigger('mouseleave')
    expect(
      document.body.querySelector('[data-channel-health-toast]')
    ).toBeNull()

    // Clicking the multi-channel cycle button switches sequentially across distinct channels without opening detail modal.
    const cycleBtn = wrapper.get('[data-cycle-channel]')
    await cycleBtn.trigger('click')
    expect(wrapper.emitted('detail')).toBeUndefined()
    expect(wrapper.get('[data-channel-quote]').text()).toContain('限流通道')
    expect(
      wrapper
        .get('[data-channel-quote] [data-catalog-offer-health="limited"]')
        .text()
    ).toBe('受限')

    await cycleBtn.trigger('click')
    expect(wrapper.get('[data-channel-quote]').text()).toContain('恢复通道')
    expect(
      wrapper
        .get('[data-channel-quote] [data-catalog-offer-health="recovering"]')
        .text()
    ).toBe('恢复')

    // Wraps cleanly back to first channel.
    await cycleBtn.trigger('click')
    expect(wrapper.get('[data-channel-quote]').text()).toContain(
      'Fixture channel'
    )
    wrapper.unmount()
  })
})
