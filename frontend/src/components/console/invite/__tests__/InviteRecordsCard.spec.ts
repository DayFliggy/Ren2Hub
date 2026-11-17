import { mount } from '@vue/test-utils'
import { beforeAll, expect, it } from 'vitest'
import type { AffiliateRecord } from '@/api/affiliate'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import InviteRecordsCard from '../InviteRecordsCard.vue'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

it('shows reversals as reductions and separates credited rewards from debt offsets', () => {
  const common = {
    id: 1,
    user_id: 2,
    invitee_id: 3,
    skip_reason: '',
    source_type: 'admin_quota',
    source_id: 'operation',
    base_quota: '10000000',
    rate_bps: 2000,
    reward_quota: '1000000',
    credited_quota: '500000',
    debt_offset_quota: '500000',
    debt_added_quota: '0',
    frozen_until: 0,
    thawed_at: 0,
    created_at: 1,
    available_after: '0',
    frozen_after: '0',
    wallet_after: '0',
  }
  const rows: AffiliateRecord[] = [
    { ...common, kind: 'accrue' },
    {
      ...common,
      id: 2,
      kind: 'reversal',
      debt_offset_quota: '0',
      debt_added_quota: '500000',
    },
    {
      ...common,
      id: 3,
      kind: 'accrue',
      reward_quota: '0',
      skip_reason: 'compliance_unconfirmed',
    },
    {
      ...common,
      id: 4,
      invitee_id: 0,
      kind: 'deduction',
      source_type: 'admin_deduction',
      reward_quota: '1500000',
      credited_quota: '0',
      debt_offset_quota: '0',
    },
  ]
  const wrapper = mount(InviteRecordsCard, {
    props: { records: rows, page: 1, total: 4, loading: false, error: '' },
    global: { plugins: [i18n] },
  })
  expect(wrapper.get('[data-record-kind="reversal"]').text()).toContain(
    '−$2.00'
  )
  expect(wrapper.get('[data-record-kind="reversal"]').text()).toContain(
    'Revoked from untransferred rewards: $1.00'
  )
  expect(wrapper.get('[data-record-kind="reversal"]').text()).toContain(
    'Added to pending offset: $1.00'
  )
  expect(wrapper.get('[data-record-kind="accrue"]').text()).toContain(
    'Credited: $1.00'
  )
  expect(wrapper.get('[data-record-kind="accrue"]').text()).toContain(
    'Applied to pending offset: $1.00'
  )
  expect(wrapper.text()).toContain('Payment compliance has not been confirmed')
  const deduction = wrapper.get('[data-record-kind="deduction"]')
  expect(deduction.text()).toContain('Deducted by administrator')
  expect(deduction.text()).toContain('−$3.00')
  // A deduction names no invitee and has no credited or revoked breakdown.
  expect(deduction.text()).not.toContain('Invited user')
  expect(deduction.text()).not.toContain('Credited')
  expect(deduction.text()).not.toContain('Revoked')
  wrapper.unmount()
})
it('uses a compact empty state without an empty pagination area', () => {
  const wrapper = mount(InviteRecordsCard, {
    props: { records: [], page: 1, total: 0, loading: false, error: '' },
    global: { plugins: [i18n] },
  })
  const empty = wrapper.get('[data-invite-records-empty]')
  expect(empty.classes()).toContain('py-3')
  expect(empty.text()).toBe('No reward records yet')
  expect(wrapper.find('nav').exists()).toBe(false)
  wrapper.unmount()
})
