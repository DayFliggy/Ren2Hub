import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import type { MarketPricing } from '@/types/modelMarket'
import ModelPriceDisplay from '../ModelPriceDisplay.vue'

function cacheWriteLabel(cacheWriteMode?: string) {
  const pricing: MarketPricing = {
    id: 'cache-model',
    billingMode: 'token',
    endpointTypes: ['openai'],
    cacheWriteMode,
    priceLines: [
      { key: 'cacheWrite', amount: '3.75', unit: 'per_million_tokens' },
    ],
  }
  const wrapper = mount(ModelPriceDisplay, {
    props: { pricing, mode: 'full' },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
      ],
    },
  })
  const text = wrapper.get('[data-price-dimension="cacheWrite"]').text()
  wrapper.unmount()
  return text
}

describe('ModelPriceDisplay cache write labels', () => {
  it('names the base cache write duration of each TTL mode', () => {
    expect(cacheWriteLabel('claude_ttl')).toContain('Cache write · 5m')
    expect(cacheWriteLabel('openai_ttl')).toContain('Cache write · 30m')
    expect(cacheWriteLabel('standard')).toContain('Cache write')
    expect(cacheWriteLabel('standard')).not.toContain('·')
  })
})
