<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { opsMonitorApi, type PerformanceAction } from '@/api/opsMonitor'
import { getModelCatalog } from '@/api/modelCatalog'
import OpsSystemTasksCard from '@/components/admin/ops/OpsSystemTasksCard.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import { usePolledFeed } from '@/composables/usePolledFeed'
import { nativeOpsMessages } from '@/i18n/nativeOps'
import '@/components/admin/ops/ops.css'
const { t } = useI18n({ useScope: 'local', messages: nativeOpsMessages })
const feed = usePolledFeed(
  async (signal) => {
    const [stats, routing, catalog] = await Promise.all([
      opsMonitorApi.stats(signal),
      opsMonitorApi.routing(signal),
      getModelCatalog(undefined, signal),
    ])
    return {
      stats,
      routing,
      names: new Map(
        catalog.channels.map((channel) => [channel.id, channel.name])
      ),
    }
  },
  () => 30000
)
const action = ref<PerformanceAction | null>(null)
const actionLoading = ref(false)
const actionError = ref('')
async function execute() {
  if (!action.value || actionLoading.value) return
  actionLoading.value = true
  actionError.value = ''
  try {
    await opsMonitorApi.execute(action.value)
    action.value = null
    await feed.refresh()
  } catch (error) {
    actionError.value = error instanceof Error ? error.message : String(error)
  } finally {
    actionLoading.value = false
  }
}
const data = computed(() => feed.data.value)
const memory = computed(() => data.value?.stats.memory_stats)
const memoryItems = computed(() => [
  {
    label: t('nativeOps.alloc'),
    value: memory.value
      ? `${(memory.value.alloc / 1048576).toFixed(1)} MiB`
      : '—',
  },
  {
    label: t('nativeOps.sys'),
    value: memory.value
      ? `${(memory.value.sys / 1048576).toFixed(1)} MiB`
      : '—',
  },
  {
    label: t('nativeOps.goroutines'),
    value: memory.value?.num_goroutine ?? '—',
  },
  { label: t('nativeOps.gc'), value: memory.value?.num_gc ?? '—' },
])
</script>
<template>
  <div class="ops-page" data-ops-page>
    <PageBreadcrumb
      class="ops-area ops-area--full"
      :crumbs="[t('nativeOps.title')]"
      ><template #action
        ><ConsoleButton
          variant="secondary"
          :loading="feed.loading.value"
          @click="feed.refresh"
          >{{ t('nativeOps.refresh') }}</ConsoleButton
        ></template
      ></PageBreadcrumb
    >
    <ErrorBanner
      v-if="feed.error.value"
      class="ops-area ops-area--full"
      :message="feed.error.value"
      @retry="feed.refresh"
    />
    <ErrorBanner
      v-if="actionError"
      class="ops-area ops-area--full"
      :message="actionError"
    />
    <div class="ops-area ops-area--full flex flex-wrap gap-3">
      <ConsoleButton
        v-for="kind in ['gc', 'disk_cache', 'reset_stats'] as const"
        :key="kind"
        variant="secondary"
        :disabled="actionLoading"
        @click="action = kind"
        >{{ t(`nativeOps.actions.${kind}`) }}</ConsoleButton
      >
    </div>
    <ConsoleCard class="ops-area ops-area--full" :title="t('nativeOps.memory')"
      ><dl class="grid grid-cols-2 gap-5 sm:grid-cols-4">
        <div v-for="item in memoryItems" :key="item.label">
          <dt class="text-xs text-[var(--text-tertiary)]">{{ item.label }}</dt>
          <dd class="display-number mt-2 text-xl">{{ item.value }}</dd>
        </div>
      </dl></ConsoleCard
    >
    <ConsoleCard
      class="ops-area ops-area--quality"
      :title="t('nativeOps.channels')"
      ><p class="mb-3 text-xs text-[var(--text-tertiary)]">
        {{ t('nativeOps.snapshot') }}
      </p>
      <div
        v-for="channel in data?.routing.channels ?? []"
        :key="channel.id"
        class="flex justify-between border-t border-[var(--border-subtle)] py-2 text-sm"
      >
        <span>{{ data?.names.get(channel.id) ?? `#${channel.id}` }}</span
        ><span>{{ channel.in_flight }} / {{ channel.limit || '∞' }}</span>
      </div></ConsoleCard
    >
    <ConsoleCard
      class="ops-area ops-area--upstream"
      :title="t('nativeOps.users')"
      ><div
        v-for="user in data?.routing.users ?? []"
        :key="user.id"
        class="flex justify-between border-t border-[var(--border-subtle)] py-2 text-sm"
      >
        <span>#{{ user.id }}</span
        ><span>{{ user.in_flight }} / {{ user.limit || '∞' }}</span>
      </div></ConsoleCard
    >
    <ConsoleCard
      class="ops-area ops-area--full"
      :title="t('nativeOps.observations')"
      ><div class="overflow-x-auto">
        <table class="w-full text-left text-sm">
          <thead>
            <tr>
              <th>{{ t('nativeOps.channels') }}</th>
              <th>{{ t('nativeOps.model') }}</th>
              <th>{{ t('nativeOps.samples') }}</th>
              <th>{{ t('nativeOps.errors') }}</th>
              <th>{{ t('nativeOps.latency') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="item in data?.routing.observations ?? []"
              :key="`${item.channel_id}:${item.model}:${item.stream}`"
            >
              <td>
                {{ data?.names.get(item.channel_id) ?? `#${item.channel_id}` }}
              </td>
              <td>{{ item.model }} {{ item.stream ? 'SSE' : '' }}</td>
              <td>{{ item.samples }}</td>
              <td>
                {{
                  item.known
                    ? `${(item.error_rate * 100).toFixed(1)}%`
                    : t('nativeOps.unknown')
                }}
              </td>
              <td>
                {{
                  item.known
                    ? `${item.latency_ms.toFixed(0)} ms`
                    : t('nativeOps.unknown')
                }}
              </td>
            </tr>
          </tbody>
        </table>
      </div></ConsoleCard
    >
    <ConsoleCard
      v-if="data"
      class="ops-area ops-area--full"
      :title="t('nativeOps.cache')"
    >
      <pre class="overflow-auto text-xs">{{
        JSON.stringify(
          {
            cache: data.stats.cache_stats,
            disk: data.stats.disk_space_info,
            files: data.stats.disk_cache_info,
            config: data.stats.config,
          },
          null,
          2
        )
      }}</pre>
    </ConsoleCard>
    <div class="ops-area ops-area--full"><OpsSystemTasksCard /></div>
    <ConfirmDialog
      :open="!!action"
      :title="action ? t(`nativeOps.actions.${action}`) : t('nativeOps.title')"
      :message="t('nativeOps.confirm')"
      :loading="actionLoading"
      @confirm="execute"
      @cancel="action = null"
    />
  </div>
</template>
