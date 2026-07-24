import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'

import KeyFormModal from '@/components/console/keys/KeyFormModal.vue'
import i18n, { loadMessageDomain } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  i18n.global.locale.value = 'zh-CN'
})

describe('KeyFormModal', () => {
  it('offers only manual and automatic token types', () => {
    const wrapper = mount(KeyFormModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: { open: true, editing: null, models: [] },
    })

    const typeOptions = [...document.body.querySelectorAll('[role="radio"]')]
    expect(typeOptions).toHaveLength(2)
    expect(typeOptions.map((option) => option.textContent)).toEqual([
      expect.stringContaining('手动令牌'),
      expect.stringContaining('自动令牌'),
    ])
    expect(typeOptions[0].getAttribute('aria-checked')).toBe('true')

    wrapper.unmount()
  })
})
