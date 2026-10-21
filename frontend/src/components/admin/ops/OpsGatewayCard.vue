<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  seriesRange,
  type OpsOverview,
  type OpsSeriesField,
} from '@/api/opsMonitor'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { formatCount } from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import OpsTrendChart from './OpsTrendChart.vue'

const props = defineProps<{
  overview: OpsOverview | null
  loading: boolean
  error: string
  updatedAt: number
}>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})
const trends = computed(() => {
  const overview = props.overview
  if (!overview?.gateway) return null
  const line = (field: OpsSeriesField) =>
    overview.series.points.map((point) => ({
      ts: point.ts,
      value: point[field],
    }))
  return {
    range: seriesRange(overview),
    bucketSeconds: overview.series.bucket_seconds,
    window: t('scope.window', {
      window: t(`window.${overview.series.window}`),
    }),
    users: [
      {
        name: t('native.userInFlight'),
        color: 'signal' as const,
        points: line('users'),
      },
    ],
    channels: [
      {
        name: t('native.channelInFlight'),
        color: 'accent' as const,
        points: line('channels'),
      },
    ],
  }
})
const counters = computed(() => [
  {
    key: 'users',
    title: t('native.userLimits'),
    rows: props.overview?.gateway?.users ?? [],
  },
  {
    key: 'channels',
    title: t('native.channelLimits'),
    rows: props.overview?.gateway?.channels ?? [],
  },
])
</script>

<template>
  <OpsCard
    :title="t('gateway.title')"
    :loading="loading"
    :loaded="overview?.gateway != null"
    :error="error"
    :updated-at="updatedAt"
    :retryable="false"
    data-ops-section="gateway"
  >
    <p v-if="trends" class="ops-note">{{ t('native.sessionHistory') }}</p>
    <div v-if="trends" class="ops-gateway-grid">
      <div class="ops-trend">
        <p class="ops-trend-title">
          {{ t('native.userInFlight')
          }}<span class="ops-scope">{{ trends.window }}</span>
        </p>
        <OpsTrendChart
          :label="t('native.userInFlight')"
          :series="trends.users"
          :bucket-seconds="trends.bucketSeconds"
          :range="trends.range"
          :height="110"
        />
      </div>
      <div class="ops-trend">
        <p class="ops-trend-title">
          {{ t('native.channelInFlight')
          }}<span class="ops-scope">{{ trends.window }}</span>
        </p>
        <OpsTrendChart
          :label="t('native.channelInFlight')"
          :series="trends.channels"
          :bucket-seconds="trends.bucketSeconds"
          :range="trends.range"
          :height="110"
        />
      </div>
      <div
        v-for="counter in counters"
        :key="counter.key"
        class="ops-trend"
        :data-ops-concurrency="counter.key"
      >
        <p class="ops-trend-title">
          {{ counter.title
          }}<span class="ops-scope">{{ t('scope.instant') }}</span>
        </p>
        <p v-if="!counter.rows.length" class="ops-empty">
          {{ t('native.noOccupancy') }}
        </p>
        <dl
          v-else
          class="ops-counter-list ops-counter-list--scroll"
          tabindex="0"
          :aria-label="counter.title"
        >
          <div v-for="row in counter.rows" :key="row.id">
            <dt>#{{ row.id }}</dt>
            <dd>
              {{ formatCount(row.in_flight, locale) }} /
              {{
                row.limit === 0
                  ? t('storage.unlimited')
                  : formatCount(row.limit, locale)
              }}
            </dd>
          </div>
        </dl>
      </div>
    </div>
  </OpsCard>
</template>
