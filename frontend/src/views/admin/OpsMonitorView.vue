<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { OPS_WINDOWS, opsMonitorApi, type OpsWindow } from '@/api/opsMonitor'
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
// The overview feeds the status, runtime and storage sections; the window
// only changes its trend series.
const overview = usePolledFeed(
  (signal) => opsMonitorApi.overview(selectedWindow.value, signal),
  () => OVERVIEW_INTERVAL_MS
)
watch(selectedWindow, () => void overview.refresh())
const overviewState = computed(() => ({
  overview: overview.data.value,
  loading: overview.loading.value,
  error: overview.error.value,
  updatedAt: overview.updatedAt.value,
}))
// Sections the user may not read are not mounted, so they never request.
const canReadChannels = computed(() => auth.hasPermission('channel', 'read'))
</script>

<template>
  <div class="ops-page" data-ops-page>
    <OpsStatusHeader
      class="ops-area ops-area--full"
      v-bind="overviewState"
      :refresh-interval-seconds="OVERVIEW_INTERVAL_MS / 1000"
      @refresh="overview.refresh"
    />
    <OpsQualityCard
      class="ops-area ops-area--quality"
      :class="{ 'ops-area--full': !canReadChannels }"
    />
    <OpsUpstreamCard
      v-if="canReadChannels"
      class="ops-area ops-area--upstream"
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
    <OpsGatewayCard class="ops-area ops-area--full" v-bind="overviewState" />
    <OpsResourcesCard
      class="ops-area ops-area--resources"
      v-bind="overviewState"
    />
    <OpsStorageCards
      class="ops-area ops-area--storage"
      v-bind="overviewState"
    />
    <div v-if="auth.isRoot" class="ops-area ops-area--full">
      <OpsSystemTasksCard />
    </div>
  </div>
</template>
