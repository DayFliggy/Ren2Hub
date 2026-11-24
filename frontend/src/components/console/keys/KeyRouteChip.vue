<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import StatusChip from '@/components/common/StatusChip.vue'
import type { TokenSummary } from '@/types/console'

const props = defineProps<{
  token: Pick<TokenSummary, 'type' | 'selected_only' | 'route_channel_count'>
}>()
const { t } = useI18n()

// The scope comes from selected_only; an empty selected list means the
// chosen channels were deleted and the backend rejects calls.
const unavailable = computed(
  () => props.token.selected_only && props.token.route_channel_count === 0
)
const label = computed(() => {
  const { type, selected_only, route_channel_count: count } = props.token
  if (!selected_only) return t('keys.route.autoPlatform')
  if (unavailable.value) return t('keys.route.unavailable')
  const key =
    type === 'ordered' ? 'keys.route.ordered' : 'keys.route.autoSelected'
  return t(key, { count }, count)
})
const tone = computed(() => {
  if (unavailable.value) return 'warning'
  return props.token.type === 'ordered' ? 'accent' : 'info'
})
</script>

<template>
  <StatusChip
    class="key-route-chip relative"
    :tone="tone"
    :data-route-warning="unavailable ? '' : undefined"
    :title="unavailable ? t('keys.route.unavailableHint') : undefined"
  >
    {{ label
    }}<span v-if="unavailable" class="sr-only">{{
      t('keys.route.unavailableHint')
    }}</span>
  </StatusChip>
</template>

<style scoped>
/* B1 Studio: route-type labels are auxiliary and stay neutral across the table,
   mobile cards, and teleported popover; deleted-channel warnings keep status
   colour. !important overrides the chip's inline tone style. */
:global(html.dark .key-route-chip:not([data-route-warning])) {
  background: var(--surface-muted) !important;
  color: var(--text-secondary) !important;
  border-left-color: var(--border-default) !important;
}
</style>
