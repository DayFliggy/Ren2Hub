<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import KeyQuotaRing from '@/components/console/keys/KeyQuotaRing.vue'
import type { TokenSummary } from '@/types/console'
import { formatQuota } from '@/utils/format'
import { tokenMeter } from '@/utils/tokenMeter'

/**
 * The quota ring that stands in for the balance figures. It is the button
 * that opens the shared detail popover, so its name carries the figures the
 * ring replaced.
 */
const props = defineProps<{
  token: TokenSummary
  now: number
  expanded: boolean
  detailId: string
}>()
const emit = defineEmits<{
  detail: [anchor: HTMLElement, pointerType: string]
  blur: []
}>()

const { t } = useI18n()
const meter = computed(() => tokenMeter(props.token, props.now))
const label = computed(() =>
  t('keys.meter.ringLabel', {
    name: props.token.name,
    remaining: props.token.unlimited
      ? t('common.unlimited')
      : formatQuota(props.token.remain_quota, 3),
    used: formatQuota(props.token.used_quota, 3),
  })
)

function openDetail(event: MouseEvent) {
  // Keyboard and synthetic clicks carry no pointer type.
  const pointerType = 'pointerType' in event ? String(event.pointerType) : ''
  emit('detail', event.currentTarget as HTMLElement, pointerType)
}
</script>

<template>
  <button
    type="button"
    class="key-quota-trigger relative inline-flex shrink-0 rounded-full align-middle focus-ring"
    :class="{ 'is-expanded': expanded }"
    data-key-quota-ring
    :data-token-id="token.id"
    :aria-label="label"
    :aria-expanded="expanded"
    :aria-controls="expanded ? detailId : undefined"
    @click.stop="openDetail"
    @blur="emit('blur')"
  >
    <KeyQuotaRing :meter="meter" :unlimited="token.unlimited" />
  </button>
</template>

<style scoped>
/* Ties the open popover, which hangs below the row, back to its ring. */
.key-quota-trigger.is-expanded {
  background: var(--state-hover-layer);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--signal) 28%, transparent);
}

/* A 44px touch target without moving the 28px glyph off its grid. */
@media (pointer: coarse) {
  .key-quota-trigger::before {
    content: '';
    position: absolute;
    inset: -8px;
  }
}
</style>
