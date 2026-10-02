<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { opsMonitorApi } from '@/api/opsMonitor'
import StatusChip from '@/components/common/StatusChip.vue'
import { usePolledFeed } from '@/composables/usePolledFeed'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import {
  formatCount,
  formatLatencyMs,
  formatRelative,
  formatTimestamp,
} from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import OpsMetric from './OpsMetric.vue'

const INTERVAL_MS = 30_000

const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})
const feed = usePolledFeed(opsMonitorApi.channels, () => INTERVAL_MS)
const report = computed(() => feed.data.value)
const latency = (value: number | null) => {
  const reading = formatLatencyMs(value, locale.value)
  return reading.value === '--' ? '--' : `${reading.value} ${reading.unit}`
}
</script>

<template>
  <OpsCard
    :title="t('upstream.title')"
    :scope="t('scope.instant')"
    :loading="feed.loading.value"
    :loaded="report !== null"
    :error="feed.error.value"
    :updated-at="feed.updatedAt.value"
    data-ops-section="upstream"
    @retry="feed.refresh"
  >
    <template v-if="report">
      <p class="ops-trend-title">{{ t('upstream.channels') }}</p>
      <dl class="ops-metric-grid ops-metric-grid--4">
        <OpsMetric
          :label="t('upstream.channels')"
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
      <div v-if="report.channels.total > 0" class="ops-channel-progress">
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
      <p class="ops-trend-title">{{ t('upstream.breakers') }}</p>
      <dl class="ops-metric-grid ops-metric-grid--3">
        <OpsMetric
          :label="t('upstream.closed')"
          :value="formatCount(report.breakers.closed, locale)"
          size="md"
        />
        <OpsMetric
          :label="t('upstream.open')"
          :value="formatCount(report.breakers.open, locale)"
          :status="report.breakers.open > 0 ? 'critical' : null"
          size="md"
        />
        <OpsMetric
          :label="t('upstream.half_open')"
          :value="formatCount(report.breakers.half_open, locale)"
          :status="report.breakers.half_open > 0 ? 'warning' : null"
          size="md"
        />
      </dl>
      <p class="ops-trend-title">{{ t('upstream.cooling') }}</p>
      <p v-if="!report.unhealthy.length" class="ops-all-clear" data-ops-empty>
        <span
          class="ops-dot"
          style="background: var(--status-success)"
          aria-hidden="true"
        />{{ t('upstream.allHealthy') }}
      </p>
      <ul
        v-else
        class="ops-cooling"
        :aria-label="t('upstream.listLabel')"
        tabindex="0"
        data-ops-cooling
      >
        <li
          v-for="item in report.unhealthy"
          :key="`${item.channel_id}:${item.model}`"
        >
          <div class="ops-cooling-head">
            <strong :title="item.channel_name">{{
              item.channel_name ||
              t('upstream.channelId', { id: item.channel_id })
            }}</strong>
            <StatusChip :tone="item.state === 'open' ? 'danger' : 'warning'">{{
              t(`upstream.${item.state}`)
            }}</StatusChip>
          </div>
          <p class="ops-cooling-meta">
            <code v-if="item.model">{{ item.model }}</code>
            <span>{{
              t('upstream.failures', { count: item.failure_count })
            }}</span>
            <span
              v-if="item.cooldown_until"
              :title="formatTimestamp(item.cooldown_until, locale)"
              >{{
                t('upstream.cooldownUntil', {
                  time: formatRelative(
                    item.cooldown_until,
                    feed.updatedAt.value,
                    locale
                  ),
                })
              }}</span
            >
          </p>
          <p class="ops-cooling-meta">
            <span
              >{{ t('upstream.lastLatency') }}
              {{ latency(item.last_latency_ms) }}</span
            >
            <span
              >{{ t('upstream.firstToken') }}
              {{ latency(item.first_token_latency_ms) }}</span
            >
          </p>
        </li>
      </ul>
    </template>
  </OpsCard>
</template>
