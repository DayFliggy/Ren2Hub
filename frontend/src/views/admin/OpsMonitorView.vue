<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { opsMonitorApi, type OpsWindow } from '@/api/opsMonitor'
import OpsGatewayCard from '@/components/admin/ops/OpsGatewayCard.vue'
import OpsQualityCard from '@/components/admin/ops/OpsQualityCard.vue'
import OpsResourcesCard from '@/components/admin/ops/OpsResourcesCard.vue'
import OpsStatusHeader from '@/components/admin/ops/OpsStatusHeader.vue'
import OpsStorageCards from '@/components/admin/ops/OpsStorageCards.vue'
import OpsSystemTasksCard from '@/components/admin/ops/OpsSystemTasksCard.vue'
import OpsUpstreamCard from '@/components/admin/ops/OpsUpstreamCard.vue'
import { usePolledFeed } from '@/composables/usePolledFeed'
import { useAuthStore } from '@/stores/auth'
import '@/components/admin/ops/ops.css'

const OVERVIEW_INTERVAL_MS = 10_000

const auth = useAuthStore()
const selectedWindow = ref<OpsWindow>('1h')
// The overview feeds the header, resources, gateway and storage sections.
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
      v-model:window="selectedWindow"
      class="ops-area ops-area--header"
      v-bind="overviewState"
      @refresh="overview.refresh"
    />
    <OpsResourcesCard
      class="ops-area ops-area--resources"
      v-bind="overviewState"
    />
    <OpsGatewayCard class="ops-area ops-area--gateway" v-bind="overviewState" />
    <OpsQualityCard
      class="ops-area ops-area--quality"
      :class="{ 'ops-area--full': !canReadChannels }"
    />
    <OpsUpstreamCard
      v-if="canReadChannels"
      class="ops-area ops-area--upstream"
    />
    <OpsStorageCards
      class="ops-area ops-area--storage"
      v-bind="overviewState"
    />
    <template v-if="auth.isRoot">
      <div class="ops-area ops-area--tasks ops-area--full">
        <OpsSystemTasksCard />
      </div>
    </template>
  </div>
</template>
