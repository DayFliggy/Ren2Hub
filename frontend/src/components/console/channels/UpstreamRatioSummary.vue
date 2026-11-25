<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChannelUpstreamRatio } from '@/api/channelUpstreamRatio'
import StatusChip from '@/components/common/StatusChip.vue'
import {
  upstreamRatioStatusTone,
  upstreamRatioValue,
} from './upstreamRatioText'

const props = defineProps<{ view: ChannelUpstreamRatio }>()
const { t } = useI18n()
const value = computed(() => upstreamRatioValue(props.view))
const isUnsupportedEmpty = computed(
  () => props.view.kind === 'unsupported' && !value.value
)
const label = computed(() => {
  if (isUnsupportedEmpty.value)
    return t('channels.upstreamRatio.unsupportedManual')
  if (props.view.kind === 'detect' && !props.view.status)
    return t('channels.upstreamRatio.notDetected')
  return t(`channels.upstreamRatio.statuses.${props.view.status || 'none'}`)
})
</script>

<template>
  <template v-if="isUnsupportedEmpty">
    <span
      class="text-[var(--text-tertiary)] underline decoration-dotted underline-offset-2"
      :title="label"
      >{{ t('channels.upstreamRatio.setManualShort') }}</span
    >
  </template>
  <template v-else>
    <span class="tabular-nums">{{
      value
        ? t('channels.upstreamRatio.cell', { ratio: value })
        : t('channels.upstreamRatio.cellEmpty')
    }}</span>
    <StatusChip
      v-if="view.status !== 'ok' || !value"
      :tone="upstreamRatioStatusTone(view.status)"
      >{{ label }}</StatusChip
    >
    <StatusChip v-if="view.stale" tone="warning">{{
      t('channels.upstreamRatio.stale')
    }}</StatusChip>
    <span v-if="view.mode === 'manual'">{{
      t('channels.upstreamRatio.modes.manual')
    }}</span>
    <span v-else-if="view.mapping">{{
      t(`channels.upstreamRatio.mappings.${view.mapping}`)
    }}</span>
  </template>
</template>
