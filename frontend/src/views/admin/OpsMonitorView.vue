<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  OPS_WINDOWS,
  appendOpsSample,
  concurrencyTotal,
  opsMonitorApi,
  projectOpsOverview,
  type PerformanceStats,
  type RoutingSnapshot,
  type OpsSeriesPoint,
  type OpsWindow,
} from '@/api/opsMonitor'
import { getModelCatalog } from '@/api/modelCatalog'
import OpsGatewayCard from '@/components/admin/ops/OpsGatewayCard.vue'
import OpsQualityCard from '@/components/admin/ops/OpsQualityCard.vue'
import OpsResourcesCard from '@/components/admin/ops/OpsResourcesCard.vue'
import OpsStatusHeader from '@/components/admin/ops/OpsStatusHeader.vue'
import OpsStorageCards from '@/components/admin/ops/OpsStorageCards.vue'
import OpsSystemTasksCard from '@/components/admin/ops/OpsSystemTasksCard.vue'
import OpsUpstreamCard from '@/components/admin/ops/OpsUpstreamCard.vue'
import SectionHeading from '@/components/common/SectionHeading.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import { usePolledFeed } from '@/composables/usePolledFeed'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { useAuthStore } from '@/stores/auth'
import '@/components/admin/ops/ops.css'

const OVERVIEW_INTERVAL_MS = 30_000

const auth = useAuthStore()
const { t } = useI18n({ useScope: 'local', messages: opsMonitorMessages })
const selectedWindow = ref<OpsWindow>('1h')
const windowOptions = computed(() =>
  OPS_WINDOWS.map((value) => ({ value, label: t(`window.${value}`) }))
)
const performancePoints = ref<OpsSeriesPoint[]>([])
const routingPoints = ref<OpsSeriesPoint[]>([])
// Each feed owns its timeline. A peer completing in a different second must
// neither insert a false gap nor republish an old measurement on this line.
function sampleFeed<T>(
  fetch: (signal: AbortSignal) => Promise<T>,
  record: (data: T | null) => void
) {
  return usePolledFeed(
    async (signal) => {
      try {
        const value = await fetch(signal)
        if (!signal.aborted) record(value)
        return value
      } catch (cause) {
        if (!signal.aborted) record(null)
        throw cause
      }
    },
    () => OVERVIEW_INTERVAL_MS
  )
}
function recordPerformance(data: PerformanceStats | null) {
  const now = Math.floor(Date.now() / 1000)
  const projected = projectOpsOverview(
    data,
    null,
    selectedWindow.value,
    [],
    now
  )
  performancePoints.value = appendOpsSample(performancePoints.value, {
    ts: now,
    cpu: null,
    memory: null,
    disk: projected?.resources?.disk.percent ?? null,
    users: null,
    channels: null,
  })
}
function recordRouting(data: RoutingSnapshot | null) {
  const now = Math.floor(Date.now() / 1000)
  routingPoints.value = appendOpsSample(routingPoints.value, {
    ts: now,
    cpu: null,
    memory: null,
    disk: null,
    users: data ? concurrencyTotal(data.users) : null,
    channels: data ? concurrencyTotal(data.channels) : null,
  })
}
const performance = sampleFeed(opsMonitorApi.stats, recordPerformance)
const routing = sampleFeed(opsMonitorApi.routing, recordRouting)
const catalog = usePolledFeed(
  (signal) => getModelCatalog(undefined, signal),
  () => OVERVIEW_INTERVAL_MS
)
const sampledAt = computed(() =>
  Math.floor(
    Math.max(performance.updatedAt.value, routing.updatedAt.value) / 1000
  )
)
const overview = computed(() =>
  projectOpsOverview(
    performance.data.value,
    routing.data.value,
    selectedWindow.value,
    [],
    sampledAt.value
  )
)
const overviewState = computed(() => ({
  overview: overview.value,
  loading: performance.loading.value || routing.loading.value,
  error: [performance.error.value, routing.error.value]
    .filter(Boolean)
    .join(' · '),
  updatedAt: Math.min(performance.updatedAt.value, routing.updatedAt.value),
}))
const performanceState = computed(() => ({
  overview: projectOpsOverview(
    performance.data.value,
    null,
    selectedWindow.value,
    performancePoints.value,
    performancePoints.value.at(-1)?.ts ?? 0
  ),
  loading: performance.loading.value,
  error: performance.error.value,
  updatedAt: performance.updatedAt.value,
}))
const gatewayState = computed(() => ({
  overview: projectOpsOverview(
    null,
    routing.data.value,
    selectedWindow.value,
    routingPoints.value,
    routingPoints.value.at(-1)?.ts ?? 0
  ),
  loading: routing.loading.value,
  error: routing.error.value,
  updatedAt: routing.updatedAt.value,
}))
const routingState = computed(() => ({
  snapshot: routing.data.value,
  loading: routing.loading.value,
  error: routing.error.value,
  updatedAt: routing.updatedAt.value,
}))
const catalogState = computed(() => ({
  catalog: catalog.data.value,
  snapshot: routing.data.value,
  loading: catalog.loading.value || routing.loading.value,
  error: [catalog.error.value, routing.error.value].filter(Boolean).join(' · '),
  updatedAt: Math.min(catalog.updatedAt.value, routing.updatedAt.value),
}))
async function refreshAll() {
  await Promise.all([
    performance.refresh(),
    routing.refresh(),
    catalog.refresh(),
  ])
}
// The user-readable model catalog is shared with routing views; channel-read
// permission controls the detailed upstream card on this Root-only page.
const canReadChannels = computed(() => auth.hasPermission('channel', 'read'))
</script>

<template>
  <div class="ops-page" data-ops-page>
    <OpsStatusHeader
      class="ops-area ops-area--full"
      v-bind="overviewState"
      :refresh-interval-seconds="OVERVIEW_INTERVAL_MS / 1000"
      @refresh="refreshAll"
    />
    <OpsQualityCard
      class="ops-area ops-area--quality"
      :class="{ 'ops-area--full': !canReadChannels }"
      v-bind="routingState"
      @retry="routing.refresh"
    />
    <OpsUpstreamCard
      v-if="canReadChannels"
      class="ops-area ops-area--upstream"
      v-bind="catalogState"
      @retry="refreshAll"
    />
    <div class="ops-area ops-area--full ops-runtime-heading">
      <SectionHeading>{{ t('runtime') }}</SectionHeading>
      <div class="ops-window">
        <span class="ops-window-note">{{ t('window.note') }}</span>
        <SegmentedToggle
          v-model="selectedWindow"
          :options="windowOptions"
          :label="t('window.label')"
          size="sm"
        />
      </div>
    </div>
    <OpsGatewayCard class="ops-area ops-area--full" v-bind="gatewayState" />
    <OpsResourcesCard
      class="ops-area ops-area--resources"
      v-bind="performanceState"
    />
    <OpsStorageCards
      class="ops-area ops-area--storage"
      v-bind="performanceState"
      @refresh="performance.refresh"
    />
    <div v-if="auth.isRoot" class="ops-area ops-area--full">
      <OpsSystemTasksCard />
    </div>
  </div>
</template>
