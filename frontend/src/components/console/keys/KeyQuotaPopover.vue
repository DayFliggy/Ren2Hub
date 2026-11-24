<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useEventListener } from '@vueuse/core'
import { useI18n } from 'vue-i18n'

import KeyMeterNote from '@/components/console/keys/KeyMeterNote.vue'
import KeyQuotaDetailList from '@/components/console/keys/KeyQuotaDetailList.vue'
import KeyQuotaRing from '@/components/console/keys/KeyQuotaRing.vue'
import KeyRouteChip from '@/components/console/keys/KeyRouteChip.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import type { TokenSummary } from '@/types/console'
import {
  tokenMeter,
  tokenMeterCentre,
  tokenMeterLabel,
  tokenMeterNote,
} from '@/utils/tokenMeter'

/**
 * The key table's one detail popover, teleported and fixed so scrolling table
 * regions cannot clip it. It opens to the right of the ring but hangs below
 * the ring's row (above when there is no room), because the actions column
 * sits right next to the ring and the hovered row's buttons must stay in
 * view. Opened by hover it never takes the pointer: it covers rows below its
 * own, and those must keep receiving hover. Pinned by a tap or key press it
 * does take the pointer, so the tap that dismisses it cannot reach a hidden
 * button underneath. Any scroll, resize, outside press or Escape closes it.
 */
const props = defineProps<{
  id: string
  token: TokenSummary | null
  anchor: HTMLElement | null
  pinned: boolean
  now: number
}>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()

/** Ring edge to popover edge, horizontally. */
const GAP = 12
/** Row edge to popover edge, vertically. */
const ROW_GAP = 4
const EDGE = 16

const panel = ref<HTMLElement | null>(null)
const position = ref<{
  top: number
  left: number
  side: 'below' | 'above' | 'over'
} | null>(null)

const meter = computed(() =>
  props.token ? tokenMeter(props.token, props.now) : null
)

const statusTone = computed(() => {
  const state = meter.value?.state
  if (state === 'exhausted') return 'danger'
  if (state === 'disabled' || state === 'expired') return 'neutral'
  return 'success'
})

// The status chip and the expiry row already carry the rest of the row's
// state line; the header adds only what needs attention.
const showNote = computed(
  () =>
    !!props.token &&
    !!meter.value &&
    tokenMeterNote(props.token, meter.value).some(
      (part) => part.tone === 'warning'
    )
)

async function place() {
  const anchor = props.anchor
  if (!props.token || !anchor) {
    position.value = null
    return
  }
  await nextTick()
  if (!panel.value) return
  const ring = anchor.getBoundingClientRect()
  const row = (anchor.closest('tr') ?? anchor).getBoundingClientRect()
  const box = panel.value.getBoundingClientRect()
  const left = Math.max(
    EDGE,
    Math.min(ring.right + GAP, innerWidth - EDGE - box.width)
  )
  const below = row.bottom + ROW_GAP
  const above = row.top - ROW_GAP - box.height
  if (below + box.height <= innerHeight - EDGE) {
    position.value = { top: below, left, side: 'below' }
  } else if (above >= EDGE) {
    position.value = { top: above, left, side: 'above' }
  } else {
    // A viewport too short for either side: overlap the row rather than clip.
    const top = Math.max(EDGE, innerHeight - EDGE - box.height)
    position.value = { top, left, side: 'over' }
  }
}

watch(() => [props.token, props.anchor], place, { immediate: true })

function close() {
  if (props.token) emit('close')
}

useEventListener(window, 'resize', close)
useEventListener(window, 'scroll', close, { capture: true, passive: true })
useEventListener(document, 'keydown', (event: KeyboardEvent) => {
  if (event.key === 'Escape') close()
})
useEventListener(
  document,
  'pointerdown',
  (event: PointerEvent) => {
    const target = event.target as Node
    if (props.anchor?.contains(target) || panel.value?.contains(target)) return
    close()
  },
  { capture: true }
)
</script>

<template>
  <Teleport to="body">
    <div
      v-if="token && meter"
      :id="id"
      ref="panel"
      role="region"
      :aria-label="t('keys.meter.detailLabel', { name: token.name })"
      data-key-quota-popover
      :data-pinned="pinned || undefined"
      :data-side="position?.side"
      data-handdrawn="menu"
      class="key-quota-popover fixed z-[300] w-[17.5rem] rounded-xl border border-[var(--overlay-border)] bg-[var(--surface-overlay)] p-3.5 text-xs text-[var(--text-secondary)]"
      :class="[
        pinned ? 'pointer-events-auto' : 'pointer-events-none',
        { 'is-placed': position },
      ]"
      :style="
        position
          ? { top: `${position.top}px`, left: `${position.left}px` }
          : { top: '0px', left: '0px', visibility: 'hidden' }
      "
      @mousedown.prevent
      @click="close"
    >
      <header class="flex items-center gap-3">
        <KeyQuotaRing
          :meter="meter"
          :unlimited="token.unlimited"
          :size="40"
          :stroke-width="4"
        >
          <span
            class="font-mono text-[0.625rem] font-semibold text-[var(--text-primary)]"
          >
            {{ tokenMeterCentre(meter) }}
          </span>
        </KeyQuotaRing>
        <div class="min-w-0">
          <p
            class="line-clamp-2 text-sm font-semibold break-all text-[var(--text-primary)]"
          >
            {{ token.name }}
          </p>
          <p class="mt-1 flex items-center gap-1.5">
            <StatusChip :tone="statusTone">
              {{ t(tokenMeterLabel(token, meter)) }}
            </StatusChip>
            <KeyRouteChip :token="token" />
          </p>
          <KeyMeterNote
            v-if="showNote"
            warnings-only
            class="mt-1 block text-[0.6875rem] leading-4"
            :token="token"
            :meter="meter"
          />
        </div>
      </header>

      <KeyQuotaDetailList
        class="mt-3 border-t border-[var(--border-subtle)] pt-3"
        :token="token"
        :meter="meter"
      />
    </div>
  </Teleport>
</template>

<style scoped>
.key-quota-popover {
  opacity: 0;
  transform: translateY(4px);
  transition:
    opacity 140ms ease-out,
    transform 140ms ease-out;
}
.key-quota-popover.is-placed {
  opacity: 1;
  transform: none;
}
@media (prefers-reduced-motion: reduce) {
  .key-quota-popover {
    transform: none;
    transition: none;
  }
}
</style>
