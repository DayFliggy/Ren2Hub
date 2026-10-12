<script setup lang="ts">
import { useResizeObserver } from '@vueuse/core'
import { ref, watch } from 'vue'

import { useTableScrollFade } from '@/composables/useTableScrollFade'

/**
 * Horizontal strip for the mobile console navigation. Items that do not fit
 * scroll sideways; an edge fade marks each side that still hides items, and
 * the current page (aria-current) is centred whenever it changes.
 */
const props = defineProps<{
  /** Identifies the current page; a change re-centres its item. */
  activeKey: string | null
  /** Background behind the strip, so the fade blends into it. */
  fadeSurface: string
}>()

defineOptions({ inheritAttrs: false })

const scroller = ref<HTMLElement | null>(null)
const { canScrollLeft, canScrollRight } = useTableScrollFade(scroller)

function centerActiveItem() {
  const strip = scroller.value
  const active = strip?.querySelector<HTMLElement>('[aria-current="page"]')
  if (!strip?.clientWidth || !active) return
  strip.scrollLeft =
    active.offsetLeft - (strip.clientWidth - active.offsetWidth) / 2
}

// The strip mounts hidden on desktop. Reveal its current item when the mobile
// layout becomes visible, without undoing manual scrolling on later resizes.
let wasVisible = false
useResizeObserver(scroller, ([entry]) => {
  const visible = entry.contentRect.width > 0
  if (visible && !wasVisible) centerActiveItem()
  wasVisible = visible
})
watch(() => props.activeKey, centerActiveItem, { flush: 'post' })
</script>

<template>
  <div class="relative">
    <!-- Attributes land on the strip itself, the direct parent of the items. -->
    <div
      ref="scroller"
      v-bind="$attrs"
      class="nav-scroller relative flex gap-1 overflow-x-auto px-3 py-1"
    >
      <slot />
    </div>
    <span
      v-if="canScrollLeft"
      class="table-scroll-fade table-scroll-fade--start"
      :style="{ '--table-fade-surface': fadeSurface }"
      aria-hidden="true"
      data-nav-scroll-fade="start"
    />
    <span
      v-if="canScrollRight"
      class="table-scroll-fade"
      :style="{ '--table-fade-surface': fadeSurface }"
      aria-hidden="true"
      data-nav-scroll-fade="end"
    />
  </div>
</template>

<style scoped>
/* The edge fades are the scroll affordance; a scrollbar would add height. */
.nav-scroller {
  scrollbar-width: none;
}
.nav-scroller::-webkit-scrollbar {
  display: none;
}
</style>
