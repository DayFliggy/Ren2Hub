<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RoutingSnapshot } from '@/api/opsMonitor'
import type { ModelCatalog } from '@/api/modelCatalog'
import StatusChip from '@/components/common/StatusChip.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import {
  formatCount,
  formatLatencyMs,
  formatPercent,
  formatRelative,
  formatTimestamp,
} from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import OpsMetric from './OpsMetric.vue'

const props = defineProps<{
  catalog: ModelCatalog | null
  snapshot: RoutingSnapshot | null
  loading: boolean
  error: string
  updatedAt: number
}>()
const emit = defineEmits<{ retry: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})
const report = computed(() => {
  if (!props.catalog) return null
  const channels = props.catalog.channels
  const observations = props.snapshot?.observations
  const known = observations?.filter((row) => row.known)
  return {
    channels: {
      total: channels.length,
      enabled: channels.filter((row) => row.status === 1).length,
      manually_disabled: channels.filter((row) => row.status === 2).length,
      auto_disabled: channels.filter((row) => row.status === 3).length,
    },
    errors: {
      normal: known
        ? known.filter((row) => row.error_rate < 0.05).length
        : null,
      elevated: known
        ? known.filter((row) => row.error_rate >= 0.05 && row.error_rate < 0.2)
            .length
        : null,
      high: known ? known.filter((row) => row.error_rate >= 0.2).length : null,
    },
    unhealthy:
      observations
        ?.filter(
          (row) =>
            (row.known && row.error_rate >= 0.05) ||
            (row.latency_known && row.latency_ms >= 3000)
        )
        .map((row) => ({
          ...row,
          channel_name: channels.find(
            (channel) => channel.id === row.channel_id
          )?.name,
          critical:
            (row.known && row.error_rate >= 0.2) ||
            (row.latency_known && row.latency_ms >= 10000),
        })) ?? [],
  }
})
const latency = (value: number | null) => {
  const reading = formatLatencyMs(value, locale.value)
  return reading.value === '--' ? '--' : `${reading.value} ${reading.unit}`
}
</script>

<template>
  <OpsCard
    :title="t('upstream.title')"
    :scope="t('scope.instant')"
    :scope-hint="t('native.catalogScope')"
    :loading="loading"
    :loaded="report !== null"
    :error="error"
    :updated-at="updatedAt"
    data-ops-section="upstream"
    @retry="emit('retry')"
  >
    <p
      v-if="report && report.channels.total === 0"
      class="ops-empty"
      data-ops-empty
    >
      {{ t('native.noPublishedChannels') }}
    </p>
    <template v-else-if="report">
      <p class="ops-trend-title">{{ t('upstream.channels') }}</p>
      <dl class="ops-metric-grid ops-metric-grid--fit">
        <OpsMetric
          :label="t('upstream.total')"
          :value="formatCount(report.channels.total, locale)"
          size="sm"
        />
        <OpsMetric
          :label="t('upstream.enabled')"
          :value="formatCount(report.channels.enabled, locale)"
          size="sm"
        />
        <OpsMetric
          :label="t('upstream.manuallyDisabled')"
          :value="formatCount(report.channels.manually_disabled, locale)"
          size="sm"
        />
        <OpsMetric
          :label="t('upstream.autoDisabled')"
          :value="formatCount(report.channels.auto_disabled, locale)"
          :status="report.channels.auto_disabled > 0 ? 'warning' : null"
          size="sm"
        />
      </dl>
      <div class="ops-channel-progress">
        <div class="ops-channel-progress-bar">
          <div
            class="ops-channel-fill ops-channel-fill--enabled"
            :style="{
              width: `${(report.channels.enabled / report.channels.total) * 100}%`,
            }"
            :title="`${t('upstream.enabled')}: ${report.channels.enabled}`"
          />
          <div
            v-if="report.channels.auto_disabled > 0"
            class="ops-channel-fill ops-channel-fill--auto"
            :style="{
              width: `${(report.channels.auto_disabled / report.channels.total) * 100}%`,
            }"
            :title="`${t('upstream.autoDisabled')}: ${report.channels.auto_disabled}`"
          />
          <div
            v-if="report.channels.manually_disabled > 0"
            class="ops-channel-fill ops-channel-fill--manual"
            :style="{
              width: `${(report.channels.manually_disabled / report.channels.total) * 100}%`,
            }"
            :title="`${t('upstream.manuallyDisabled')}: ${report.channels.manually_disabled}`"
          />
        </div>
      </div>
      <p class="ops-trend-title">{{ t('native.errorObservations') }}</p>
      <dl class="ops-metric-grid ops-metric-grid--3">
        <OpsMetric
          :label="t('native.errorNormal')"
          :value="formatCount(report.errors.normal, locale)"
          size="md"
        />
        <OpsMetric
          :label="t('native.errorHigh')"
          :value="formatCount(report.errors.high, locale)"
          :status="(report.errors.high ?? 0) > 0 ? 'critical' : null"
          size="md"
        />
        <OpsMetric
          :label="t('native.errorElevated')"
          :value="formatCount(report.errors.elevated, locale)"
          :status="(report.errors.elevated ?? 0) > 0 ? 'warning' : null"
          size="md"
        />
      </dl>
      <p class="ops-trend-title">{{ t('native.abnormalObservations') }}</p>
      <p v-if="!report.unhealthy.length" class="ops-empty" data-ops-empty>
        {{ t('native.noAbnormalObservations') }}
      </p>
      <ul
        v-else
        class="ops-cooling"
        :aria-label="t('native.abnormalObservations')"
        tabindex="0"
        data-ops-cooling
      >
        <li
          v-for="item in report.unhealthy"
          :key="`${item.channel_id}:${item.model}:${item.operation}:${item.stream}`"
        >
          <div class="ops-cooling-head">
            <strong :title="item.channel_name">{{
              item.channel_name ||
              t('upstream.channelId', { id: item.channel_id })
            }}</strong>
            <StatusChip :tone="item.critical ? 'danger' : 'warning'">{{
              t(item.critical ? 'status.critical' : 'status.warning')
            }}</StatusChip>
          </div>
          <p
            class="ops-cooling-meta"
            :aria-description="t('native.observationNote')"
            :title="t('native.observationNote')"
          >
            <code v-if="item.model">{{ item.model }}</code>
            <span
              >{{ t('native.errorEWMA') }}
              {{
                formatPercent(item.known ? item.error_rate * 100 : null, locale)
                  .value
              }}<span v-if="item.known">%</span></span
            >
            <span :title="formatTimestamp(item.updated_at / 1000, locale)">{{
              t('upstream.updated', {
                time: formatRelative(item.updated_at / 1000, updatedAt, locale),
              })
            }}</span>
          </p>
          <p class="ops-cooling-meta">
            <span
              >{{
                item.stream
                  ? t('upstream.firstToken')
                  : t('native.completionLatency')
              }}
              {{ latency(item.latency_known ? item.latency_ms : null) }}</span
            >
            <span
              >{{ t('native.sampleCount') }}
              {{ formatCount(item.samples, locale) }}</span
            >
          </p>
        </li>
      </ul>
    </template>
  </OpsCard>
</template>
