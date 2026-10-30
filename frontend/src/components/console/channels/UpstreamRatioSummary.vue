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
const label = computed(() => {
  if (props.view.kind === 'unsupported' && !value.value)
    return t('channels.upstreamRatio.unsupportedManual')
  if (props.view.kind === 'detect' && !props.view.status)
    return t('channels.upstreamRatio.notDetected')
  return t(`channels.upstreamRatio.statuses.${props.view.status || 'none'}`)
})
</script>

<template>
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
