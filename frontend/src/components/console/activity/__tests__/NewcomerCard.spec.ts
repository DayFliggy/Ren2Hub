import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import NewcomerCard from '../NewcomerCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { Activity } from '@/types/console'

const activity = {
  id: 2,
  kind: 'newcomer',
  status: 'ongoing',
  gradient: 'signal',
  start: 0,
  end: 4102444800,
  icon: 'M0 0',
  newcomer: { tasks: [], claimed: false },
} as Extract<Activity, { kind: 'newcomer' }>

// The API sends no display text for activities; the card names itself in the
// console language.
describe('newcomer activity card', () => {
  it('takes its title and tagline from the console locale', async () => {
    await loadMessageDomain('console')
    await setLocale('en')
    const wrapper = mount(NewcomerCard, {
      props: { activity, claiming: false },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    expect(wrapper.text()).toContain('Newcomer Pack')
    expect(wrapper.text()).toContain(
      'Claim rewards after finishing the starter tasks'
    )
    wrapper.unmount()
  })
})
