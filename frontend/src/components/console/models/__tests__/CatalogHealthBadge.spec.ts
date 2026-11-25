import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n, { setLocale } from '@/i18n'
import CatalogHealthBadge from '../CatalogHealthBadge.vue'
import type {
  CatalogChannelHealth,
  CatalogOfferHealth,
} from '@/api/modelCatalog'

function mountBadge(props: {
  kind?: 'offer' | 'channel'
  health?: CatalogOfferHealth
  channelSummary?: CatalogChannelHealth
  compact?: boolean
  detail?: boolean
}) {
  return mount(CatalogHealthBadge, {
    props,
    global: { plugins: [i18n] },
  })
}

describe('CatalogHealthBadge', () => {
  beforeEach(async () => {
    await setLocale('zh-CN')
  })

  it.each([
    [
      { availability: 'paused', quality: 'unrated' } as CatalogOfferHealth,
      'paused',
      '自动路由暂停',
      'is-bad',
    ],
    [
      { availability: 'limited', quality: 'unrated' } as CatalogOfferHealth,
      'limited',
      '渠道临时受限',
      'is-warn',
    ],
    [
      { availability: 'recovering', quality: 'degraded' } as CatalogOfferHealth,
      'recovering',
      '恢复观察',
      'is-warn',
    ],
    [
      {
        availability: 'no_restriction',
        quality: 'no_downgrade',
      } as CatalogOfferHealth,
      'no_downgrade',
      '未见降档',
      'is-good',
    ],
    [
      {
        availability: 'no_restriction',
        quality: 'degraded',
      } as CatalogOfferHealth,
      'degraded',
      '部分调用质量下降',
      'is-warn',
    ],
    [
      {
        availability: 'no_restriction',
        quality: 'severely_degraded',
      } as CatalogOfferHealth,
      'severely_degraded',
      '部分调用质量明显下降',
      'is-bad',
    ],
    [
      {
        availability: 'no_restriction',
        quality: 'unrated',
      } as CatalogOfferHealth,
      'unrated',
      '近期样本不足',
      'is-neutral',
    ],
    [
      { availability: 'unknown', quality: 'unrated' } as CatalogOfferHealth,
      'unknown',
      '路由状态未知',
      'is-neutral',
    ],
  ])(
    'renders offer health %o as state=%s label=%s',
    (health, expectedState, expectedText, expectedTone) => {
      const wrapper = mountBadge({ health, compact: true })
      const root = wrapper.get('[data-catalog-offer-health]')
      expect(root.attributes('data-catalog-offer-health')).toBe(expectedState)
      expect(root.text()).toContain(expectedText)
      expect(root.get('.catalog-health-pill').classes()).toContain(expectedTone)
      // Unrated offers must never display green good tone or fabricated percentages.
      if (expectedState === 'unrated') {
        expect(root.get('.catalog-health-pill').classes()).not.toContain(
          'is-good'
        )
        expect(root.text()).not.toMatch(/\d%/)
      }
    }
  )

  it('shows secondary quality warning alongside recovering availability', () => {
    const wrapper = mountBadge({
      health: { availability: 'recovering', quality: 'degraded' },
      detail: true,
    })
    expect(wrapper.text()).toContain('恢复观察')
    expect(wrapper.text()).toContain('部分调用质量下降')
    expect(wrapper.text()).toContain('不含实时并发占用')
  })

  it('falls back cleanly when server omits health on older v1 responses', () => {
    const offerWrapper = mountBadge({ compact: true })
    expect(
      offerWrapper
        .get('[data-catalog-offer-health]')
        .attributes('data-catalog-offer-health')
    ).toBe('unavailable')
    expect(offerWrapper.text()).toContain('暂无渠道观测')

    const channelWrapper = mountBadge({ kind: 'channel', compact: true })
    expect(
      channelWrapper
        .get('[data-catalog-channel-health]')
        .attributes('data-catalog-channel-health')
    ).toBe('unavailable')
    expect(channelWrapper.text()).toContain('暂无渠道观测')
  })

  it('summarizes channel visible-model health counts accurately', () => {
    const affected = mountBadge({
      kind: 'channel',
      channelSummary: {
        visible_models: 3,
        affected_models: 1,
        evaluated_models: 1,
        unrated_models: 1,
      },
      compact: true,
    })
    expect(affected.text()).toContain('1 个模型受影响')
    expect(affected.text()).toContain('1 个未见异常')
    expect(affected.text()).toContain('1 个待评估')

    const allClean = mountBadge({
      kind: 'channel',
      channelSummary: {
        visible_models: 2,
        affected_models: 0,
        evaluated_models: 2,
        unrated_models: 0,
      },
      compact: true,
    })
    expect(
      allClean
        .get('[data-catalog-channel-health]')
        .attributes('data-catalog-channel-health')
    ).toBe('clean')
    expect(allClean.text()).toContain('已评估模型未见异常')

    const allUnrated = mountBadge({
      kind: 'channel',
      channelSummary: {
        visible_models: 2,
        affected_models: 0,
        evaluated_models: 0,
        unrated_models: 2,
      },
      compact: true,
    })
    expect(allUnrated.text()).toContain('暂无可评价的模型')
  })
})
