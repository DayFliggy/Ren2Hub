import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import NavigationProgress from '../NavigationProgress.vue'
import { navigationPending } from '@/router/navigationState'

beforeEach(() => {
  vi.useFakeTimers()
  navigationPending.value = false
})

afterEach(() => {
  navigationPending.value = false
  vi.useRealTimers()
})

const bar = '[data-navigation-progress]'

it('stays hidden for a navigation that settles quickly', async () => {
  const wrapper = mount(NavigationProgress)

  navigationPending.value = true
  await nextTick()
  vi.advanceTimersByTime(100)
  navigationPending.value = false
  await nextTick()
  vi.advanceTimersByTime(500)
  await nextTick()

  expect(wrapper.find(bar).exists()).toBe(false)
})

it('appears once a navigation is slow and goes away when it settles', async () => {
  const wrapper = mount(NavigationProgress)

  navigationPending.value = true
  await nextTick()
  expect(wrapper.find(bar).exists()).toBe(false)
  vi.advanceTimersByTime(150)
  await nextTick()
  expect(wrapper.find(bar).exists()).toBe(true)

  navigationPending.value = false
  await nextTick()
  expect(wrapper.find(bar).exists()).toBe(false)
})

it('shows for a navigation that was already pending when it mounted', async () => {
  navigationPending.value = true
  const wrapper = mount(NavigationProgress)

  vi.advanceTimersByTime(150)
  await nextTick()

  expect(wrapper.find(bar).exists()).toBe(true)
})
