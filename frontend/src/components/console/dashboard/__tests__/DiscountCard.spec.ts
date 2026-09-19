import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'

import DiscountCard from '@/components/console/dashboard/DiscountCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { UserDiscounts } from '@/composables/useDashboard'

beforeAll(async () => {
  await loadMessageDomain('console')
  setLocale('en')
})

const discounts: UserDiscounts = {
  global_ratio: 0.88,
  effective_ratio: 0.88,
}

function render(props: Record<string, unknown> = {}) {
  return mount(DiscountCard, {
    props: { discounts, ...props },
    global: { plugins: [i18n] },
  })
}

describe('DiscountCard', () => {
  it('shows an explicit unavailable state without traffic price claims', () => {
    const wrapper = render({ discounts: null })

    expect(wrapper.text()).toContain('Discount details are not available')
    expect(wrapper.text()).not.toContain('Saved')
    expect(wrapper.text()).not.toContain('At list price')
  })

  it('renders the platform discount without a plan discount', () => {
    const wrapper = render()

    expect(wrapper.text()).toContain('0.88×')
    expect(wrapper.text()).not.toContain('Personal discount')
  })

  it('still shows the headline rate', () => {
    expect(render().text()).toContain('0.880×')
  })
})
