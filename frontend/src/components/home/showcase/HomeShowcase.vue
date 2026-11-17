<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useIntersectionObserver } from '@vueuse/core'
import { storeToRefs } from 'pinia'

import { useHomeShowcase } from '@/composables/useHomeShowcase'
import { useAppStore } from '@/stores'

import RuntimePulseBand from './RuntimePulseBand.vue'

const root = ref<HTMLElement | null>(null)
const app = useAppStore()
const { uptimeLabel, uptimePercent, startTime } = storeToRefs(app)
const observesVisibility =
  typeof window !== 'undefined' && 'IntersectionObserver' in window
const showcase = useHomeShowcase(startTime, {
  loadMetrics: true,
  initiallyVisible: !observesVisibility,
})

// One callback can carry several records for the same element, e.g. the
// initial hidden state and the visible one after a scroll; the last record is
// the current state.
const { stop } = useIntersectionObserver(
  root,
  (entries) =>
    showcase.setSectionVisible(Boolean(entries.at(-1)?.isIntersecting)),
  { threshold: 0 }
)

// The band's endless motion runs only while some of it shows. Under the
// full-height hero the band's edge touches the viewport, which counts as
// intersecting, so this observer's viewport ends 1px early on each side.
const motionVisible = ref(!observesVisibility)
const { stop: stopMotion } = useIntersectionObserver(
  root,
  (entries) => {
    motionVisible.value = Boolean(entries.at(-1)?.isIntersecting)
  },
  { rootMargin: '-1px 0px' }
)

onBeforeUnmount(() => {
  stop()
  stopMotion()
})
</script>

<template>
  <main ref="root" class="home-showcase no-handdrawn">
    <RuntimePulseBand
      :motion-paused="!motionVisible"
      :runtime="showcase.runtime.value"
      :uptime-label="uptimePercent === null ? null : uptimeLabel"
      :request-metrics="showcase.requestMetrics.value"
    />
  </main>
</template>
