<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import type { TokenSummary } from '@/types/console'
import { tokenMeterNote, type TokenMeter } from '@/utils/tokenMeter'

/**
 * A key's state line; only the part that needs attention is coloured.
 * `warningsOnly` keeps just those parts, for places that already show the
 * status and the expiry date elsewhere.
 */
const props = withDefaults(
  defineProps<{
    token: TokenSummary
    meter: TokenMeter
    warningsOnly?: boolean
  }>(),
  { warningsOnly: false }
)

const { t } = useI18n()

const TONE_COLOR = {
  warning: 'var(--status-warning-text)',
  danger: 'var(--status-danger-text)',
} as const

const parts = computed(() =>
  tokenMeterNote(props.token, props.meter)
    .filter((part) => !props.warningsOnly || part.tone === 'warning')
    .map((part) => ({
      text: t(part.key, part.params ?? {}),
      color: part.tone ? TONE_COLOR[part.tone] : undefined,
    }))
)
</script>

<template>
  <span class="text-[var(--text-tertiary)]" data-key-quota-note>
    <template v-for="(part, index) in parts" :key="index">
      <template v-if="index > 0"> · </template>
      <span :style="part.color ? { color: part.color } : undefined">{{
        part.text
      }}</span>
    </template>
  </span>
</template>
