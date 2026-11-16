<script setup lang="ts">
// A thin bar across the top while a navigation waits for its guards or page
// code. It appears only after a short delay, so instant navigations never
// flash it.
import { onBeforeUnmount, ref, watch } from 'vue'

import { navigationPending } from '@/router/navigationState'

const SHOW_AFTER_MS = 120
const visible = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined

watch(
  navigationPending,
  (pending) => {
    clearTimeout(timer)
    if (!pending) {
      visible.value = false
      return
    }
    timer = setTimeout(() => {
      visible.value = true
    }, SHOW_AFTER_MS)
  },
  { immediate: true }
)

onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div
    v-if="visible"
    class="navigation-progress"
    data-navigation-progress
    aria-hidden="true"
  />
</template>

<style scoped>
.navigation-progress {
  position: fixed;
  inset: 0 0 auto;
  z-index: var(--z-tooltip);
  height: 2px;
  overflow: hidden;
  background: var(--accent-soft);
  pointer-events: none;
}

/* With reduced motion the segment stays still at the left edge. */
.navigation-progress::after {
  position: absolute;
  inset: 0 auto 0 0;
  width: 40%;
  background: var(--accent);
  content: '';
  animation: navigation-progress-slide 1.1s ease-in-out infinite;
}

@keyframes navigation-progress-slide {
  from {
    transform: translateX(-100%);
  }
  to {
    transform: translateX(250%);
  }
}
</style>
