<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RoutingSnapshot } from '@/api/opsMonitor'
import type { ModelCatalog } from '@/api/modelCatalog'
import StatusChip from '@/components/common/StatusChip.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import {
  durationParts,
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

const nowMs = ref(Date.now())
let ticker: number | null = null
onMounted(() => {
  if (typeof window !== 'undefined') {
    ticker = window.setInterval(() => {
      nowMs.value = Date.now()
    }, 1000)
  }
})
onUnmounted(() => {
  if (ticker !== null) clearInterval(ticker)
})

const channelNameMap = computed(() => {
  const map: Record<number, string> = {}
  for (const ch of props.catalog?.channels ?? []) {
    if (ch.name) map[ch.id] = ch.name
  }
  for (const ch of props.snapshot?.channels ?? []) {
    if (ch.name && !map[ch.id]) map[ch.id] = ch.name
  }
  return map
})

const breakers = computed(() => props.snapshot?.breakers ?? [])
const cooldowns = computed(() => props.snapshot?.cooldowns ?? [])

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
          channel_name: channelNameMap.value[row.channel_id],
          critical:
            (row.known && row.error_rate >= 0.2) ||
            (row.latency_known && row.latency_ms >= 10000),
        })) ?? [],
  }
})

function channelDisplay(id: number, name?: string): string {
  return name || channelNameMap.value[id] || t('upstream.channelId', { id })
}

function formatRemaining(untilMs: number): string {
  const remainingSec = Math.ceil((untilMs - nowMs.value) / 1000)
  if (remainingSec <= 0) return t('upstreamLive.readyToProbe')
  const duration = durationParts(remainingSec)
    .map((part) => t(`duration.${part.unit}`, { n: part.amount }))
    .join(' ')
  return t('upstreamLive.remaining', { duration })
}

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
      <div class="ops-trend">
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
      </div>

      <div class="ops-trend" data-ops-breakers>
        <div class="ops-trend-title">
          <span>{{ t('upstreamLive.breakersTitle') }}</span>
          <StatusChip v-if="breakers.length" tone="danger">{{
            t('upstreamLive.breakersCount', { count: breakers.length })
          }}</StatusChip>
          <StatusChip v-if="cooldowns.length" tone="warning">{{
            t('upstreamLive.cooldownsCount', { count: cooldowns.length })
          }}</StatusChip>
        </div>
        <div
          v-if="!breakers.length && !cooldowns.length"
          class="ops-state-banner ops-state-banner--ok"
        >
          <span
            class="ops-dot"
            style="background: var(--status-success)"
            aria-hidden="true"
          />
          <span>{{ t('upstreamLive.allClear') }}</span>
        </div>
        <ul
          v-else
          class="ops-cooling subtle-scroll"
          :aria-label="t('upstreamLive.breakersTitle')"
          tabindex="0"
        >
          <li
            v-for="item in breakers"
            :key="`b:${item.channel_id}:${item.model}`"
          >
            <div class="ops-cooling-head">
              <strong
                :title="channelDisplay(item.channel_id, item.channel_name)"
                >{{
                  channelDisplay(item.channel_id, item.channel_name)
                }}</strong
              >
              <div class="ops-diagnosis-chips">
                <StatusChip
                  :tone="item.state === 'open' ? 'danger' : 'warning'"
                  >{{
                    item.state === 'open'
                      ? t('upstreamLive.stateOpen')
                      : t('upstreamLive.stateHalfOpen')
                  }}</StatusChip
                >
                <StatusChip v-if="item.probe_in_flight" tone="info">{{
                  t('upstreamLive.probeInFlight')
                }}</StatusChip>
              </div>
            </div>
            <p class="ops-cooling-meta">
              <code v-if="item.model">{{ item.model }}</code>
              <span v-if="item.last_status">{{
                t('upstreamLive.httpStatus', { code: item.last_status })
              }}</span>
              <code v-if="item.last_error_code">{{
                item.last_error_code
              }}</code>
              <span v-if="item.reopens > 0">{{
                t('upstreamLive.reopens', { count: item.reopens })
              }}</span>
              <span class="font-mono">{{
                formatRemaining(item.open_until)
              }}</span>
            </p>
          </li>
          <li v-for="item in cooldowns" :key="`c:${item.channel_id}`">
            <div class="ops-cooling-head">
              <strong
                :title="channelDisplay(item.channel_id, item.channel_name)"
                >{{
                  channelDisplay(item.channel_id, item.channel_name)
                }}</strong
              >
              <StatusChip tone="warning">{{
                t('upstreamLive.cooldown')
              }}</StatusChip>
            </div>
            <p class="ops-cooling-meta">
              <span v-if="item.source">{{
                t('upstreamLive.source', { source: item.source })
              }}</span>
              <span class="font-mono">{{ formatRemaining(item.until) }}</span>
            </p>
          </li>
        </ul>
      </div>

      <div class="ops-trend">
        <p class="ops-trend-title">{{ t('native.errorObservations') }}</p>
        <dl class="ops-metric-grid ops-metric-grid--3">
          <OpsMetric
            :label="t('native.errorNormal')"
            :value="formatCount(report.errors.normal, locale)"
            size="md"
          />
          <OpsMetric
            :label="t('native.errorElevated')"
            :value="formatCount(report.errors.elevated, locale)"
            :status="(report.errors.elevated ?? 0) > 0 ? 'warning' : null"
            size="md"
          />
          <OpsMetric
            :label="t('native.errorHigh')"
            :value="formatCount(report.errors.high, locale)"
            :status="(report.errors.high ?? 0) > 0 ? 'critical' : null"
            size="md"
          />
        </dl>
      </div>

      <div class="ops-trend">
        <p class="ops-trend-title">{{ t('native.abnormalObservations') }}</p>
        <p v-if="!report.unhealthy.length" class="ops-empty" data-ops-empty>
          {{ t('native.noAbnormalObservations') }}
        </p>
        <ul
          v-else
          class="ops-cooling subtle-scroll"
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
                  formatPercent(
                    item.known ? item.error_rate * 100 : null,
                    locale
                  ).value
                }}<span v-if="item.known">%</span></span
              >
              <span :title="formatTimestamp(item.updated_at / 1000, locale)">{{
                t('upstream.updated', {
                  time: formatRelative(
                    item.updated_at / 1000,
                    updatedAt,
                    locale
                  ),
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
      </div>
    </template>
  </OpsCard>
</template>
