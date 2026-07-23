import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import DataTable from '@/components/common/DataTable.vue'
import i18n from '@/i18n'

describe('DataTable keyboard rows', () => {
  it('activates clickable rows with Enter and Space but ignores child controls', async () => {
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        rows: [{ id: 1, name: 'Alpha' }],
        rowKey: 'id',
        rowClickable: true,
      },
      slots: {
        'cell-name': '<button class="child-control">Child</button>',
      },
      global: { plugins: [i18n] },
    })
    const row = wrapper.get('tbody tr')

    await row.trigger('keydown', { key: 'Enter' })
    await row.trigger('keydown', { key: ' ' })
    expect(wrapper.emitted('row-click')).toHaveLength(2)

    await wrapper.get('.child-control').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('row-click')).toHaveLength(2)
  })
})
