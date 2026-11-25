<script setup lang="ts">
import { computed } from 'vue'

import KeyQuotaFigures from '@/components/console/keys/KeyQuotaFigures.vue'
import type { TokenSummary } from '@/types/console'
import { tokenMeter } from '@/utils/tokenMeter'

/**
 * Static quota summary strip for card layouts: tinted by urgency state and
 * showing the ring plus remaining/used figures directly without an accordion.
 */
const props = defineProps<{
  token: TokenSummary
  now: number
}>()

const meter = computed(() => tokenMeter(props.token, props.now))

// Same urgency as the ring colour: blocked keys stay neutral.
const tone = computed(() => {
  if (meter.value.state === 'exhausted') return 'danger'
  if (meter.value.state === 'low') return 'warning'
  if (meter.value.expiresInDays !== null) return 'warning'
  return 'quiet'
})
</script>

<template>
  <div
    class="key-quota-bar no-handdrawn rounded-xl border px-3 py-2.5"
    :data-tone="tone"
  >
    <KeyQuotaFigures :token="token" :now="now" />
  </div>
</template>

<style scoped>
/* A quiet bar is outline only, so the two tinted states stand out from it. */
.key-quota-bar {
  border-color: var(--border-default);
  background: transparent;
}
.key-quota-bar[data-tone='warning'] {
  border-color: color-mix(in srgb, var(--status-warning-text) 25%, transparent);
  background: var(--status-warning-soft);
}
.key-quota-bar[data-tone='danger'] {
  border-color: color-mix(in srgb, var(--status-danger-text) 25%, transparent);
  background: var(--status-danger-soft);
}
</style>
