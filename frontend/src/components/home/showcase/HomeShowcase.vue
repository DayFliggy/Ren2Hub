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

// The clock, the request metrics and the band's endless motion run only while
// some of the band shows. Under the full-height hero the band's edge touches
// the viewport, which counts as intersecting, so the observed viewport ends
// 1px early on each side. One callback can carry several records for the same
// element, e.g. the initial hidden state and the visible one after a scroll;
// the last record is the current state.
const visible = ref(!observesVisibility)
const { stop } = useIntersectionObserver(
  root,
  (entries) => {
    visible.value = Boolean(entries.at(-1)?.isIntersecting)
    showcase.setSectionVisible(visible.value)
  },
  { rootMargin: '-1px 0px' }
)

onBeforeUnmount(stop)
</script>

<template>
  <main ref="root" class="home-showcase no-handdrawn">
    <RuntimePulseBand
      :motion-paused="!visible"
      :runtime="showcase.runtime.value"
      :uptime-label="uptimePercent === null ? null : uptimeLabel"
      :request-metrics="showcase.requestMetrics.value"
    />
  </main>
</template>
