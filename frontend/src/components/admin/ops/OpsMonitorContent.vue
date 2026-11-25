<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  OPS_WINDOWS,
  QUALITY_HOURS,
  opsMonitorApi,
  projectOpsOverview,
  type OpsQualityHours,
  type OpsWindow,
} from '@/api/opsMonitor'
import { getModelCatalog } from '@/api/modelCatalog'
import OpsGatewayCard from '@/components/admin/ops/OpsGatewayCard.vue'
import OpsQualityCard from '@/components/admin/ops/OpsQualityCard.vue'
import OpsResourcesCard from '@/components/admin/ops/OpsResourcesCard.vue'
import OpsStatusHeader, {
  type PollingIntervalChoice,
} from '@/components/admin/ops/OpsStatusHeader.vue'
import OpsStorageCards from '@/components/admin/ops/OpsStorageCards.vue'
import OpsSystemTasksCard from '@/components/admin/ops/OpsSystemTasksCard.vue'
import OpsUpstreamCard from '@/components/admin/ops/OpsUpstreamCard.vue'
import SectionHeading from '@/components/common/SectionHeading.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import { usePolledFeed } from '@/composables/usePolledFeed'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { useAuthStore } from '@/stores/auth'
import '@/components/admin/ops/ops.css'

const auth = useAuthStore()
const { t } = useI18n({ useScope: 'local', messages: opsMonitorMessages })

const selectedWindow = ref<OpsWindow>('1h')
const qualityHours = ref<OpsQualityHours>(QUALITY_HOURS)
const refreshIntervalSeconds = ref<PollingIntervalChoice>(30)
const pollingPaused = ref(false)

const windowOptions = computed(() =>
  OPS_WINDOWS.map((value) => ({ value, label: t(`window.${value}`) }))
)

const pollDelayMs = () =>
  pollingPaused.value ? null : refreshIntervalSeconds.value * 1000

const performance = usePolledFeed(
  (signal) => opsMonitorApi.stats(signal, selectedWindow.value),
  pollDelayMs
)
const routing = usePolledFeed(
  (signal) => opsMonitorApi.routing(signal),
  pollDelayMs
)
const quality = usePolledFeed(
  (signal) => opsMonitorApi.quality(signal, qualityHours.value),
  pollDelayMs
)
const catalog = usePolledFeed(
  (signal) => getModelCatalog(undefined, signal),
  pollDelayMs
)

watch(selectedWindow, () => {
  // A different window owns different server buckets; do not relabel old data.
  performance.data.value = null
  void performance.refresh()
})

watch(qualityHours, () => {
  quality.data.value = null
  void quality.refresh()
})

watch(pollingPaused, (paused) => {
  if (!paused) void refreshAll()
})

watch(refreshIntervalSeconds, () => {
  if (!pollingPaused.value) void refreshAll()
})

const channelNames = computed<Record<number, string>>(() => {
  const map: Record<number, string> = {}
  for (const ch of catalog.data.value?.channels ?? []) {
    if (ch.name) map[ch.id] = ch.name
  }
  for (const ch of routing.data.value?.channels ?? []) {
    if (ch.name && !map[ch.id]) map[ch.id] = ch.name
  }
  return map
})

const serverSampledAt = computed(
  () =>
    performance.data.value?.observability.sampled_at ??
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
    serverSampledAt.value
  )
)

const overviewState = computed(() => ({
  overview: overview.value,
  quality: quality.data.value,
  qualityHours: qualityHours.value,
  channelNames: channelNames.value,
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
    [],
    performance.data.value?.observability.sampled_at ??
      Math.floor(performance.updatedAt.value / 1000)
  ),
  loading: performance.loading.value,
  error: performance.error.value,
  updatedAt: performance.updatedAt.value,
}))

const gatewayState = computed(() => ({
  overview: routing.data.value
    ? projectOpsOverview(
        performance.data.value,
        routing.data.value,
        selectedWindow.value,
        [],
        serverSampledAt.value
      )
    : null,
  channelNames: channelNames.value,
  loading: routing.loading.value,
  error: routing.error.value,
  updatedAt: routing.updatedAt.value,
}))

const qualityState = computed(() => ({
  quality: quality.data.value,
  snapshot: routing.data.value,
  hours: qualityHours.value,
  loading: quality.loading.value || routing.loading.value,
  error: [quality.error.value, routing.error.value].filter(Boolean).join(' · '),
  updatedAt: Math.min(quality.updatedAt.value, routing.updatedAt.value),
}))

function refreshQuality() {
  return Promise.all([quality.refresh(), routing.refresh()])
}

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
    quality.refresh(),
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
      :refresh-interval-seconds="refreshIntervalSeconds"
      :paused="pollingPaused"
      @refresh="refreshAll"
      @update:refresh-interval-seconds="refreshIntervalSeconds = $event"
      @update:paused="pollingPaused = $event"
    />
    <OpsQualityCard
      class="ops-area ops-area--quality"
      :class="{ 'ops-area--full': !canReadChannels }"
      v-bind="qualityState"
      @retry="refreshQuality"
      @update:hours="qualityHours = $event"
    />
    <OpsUpstreamCard
      v-if="canReadChannels"
      class="ops-area ops-area--upstream"
      v-bind="catalogState"
      @retry="refreshAll"
    />
    <div class="ops-area ops-area--full ops-runtime-heading">
      <SectionHeading>{{ t('runtime') }}</SectionHeading>
      <div class="ops-window" data-ops-runtime-window>
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
