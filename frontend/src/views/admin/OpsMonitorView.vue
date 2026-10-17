<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import PageHero from '@/components/console/PageHero.vue'
import UnavailablePanel from '@/components/console/dashboard/UnavailablePanel.vue'
import StatTileGrid from '@/components/common/StatTileGrid.vue'
import { restoredConsoleMessages } from '@/i18n/restoredConsole'
import { useAuthStore } from '@/stores/auth'
import { useLatestRequest } from '@/composables/useLatestRequest'
import OpsSystemTasksCard from '@/components/admin/ops/OpsSystemTasksCard.vue'
const { t, locale } = useI18n({ messages: restoredConsoleMessages })
const auth = useAuthStore()
const request = useLatestRequest()
const updatedAt = ref<number | null>(null)
interface Stats {
  memory_stats: Record<string, number>
  cache_stats: Record<string, unknown>
  disk_cache_info: Record<string, unknown>
  disk_space_info: Record<string, unknown>
  config: Record<string, unknown>
}
const data = ref<Stats | null>(null),
  error = ref(''),
  loading = ref(false),
  action = ref<'gc' | 'disk_cache' | 'reset_stats' | null>(null)
const memoryTiles = computed(() =>
  ['alloc', 'sys', 'total_alloc', 'num_goroutine', 'num_gc'].map(
    (key, index) => {
      const value = data.value?.memory_stats[key]
      return {
        label: t(
          `restored.${['alloc', 'sys', 'totalAlloc', 'goroutines', 'collections'][index]}`
        ),
        value:
          value === undefined
            ? '—'
            : index < 3
              ? t('restored.bytes', {
                  value: value.toLocaleString(locale.value),
                })
              : value.toLocaleString(locale.value),
      }
    }
  )
)
async function load() {
  if (!auth.isRoot) return
  loading.value = true
  error.value = ''
  const result = await request.run((signal) =>
    api.get<Stats>('/api/performance/stats', undefined, { signal })
  )
  if (result.stale) return
  loading.value = false
  if (result.ok) {
    data.value = result.value
    updatedAt.value = Date.now()
  } else
    error.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
}
async function execute() {
  if (!action.value || !auth.isRoot) return
  loading.value = true
  error.value = ''
  try {
    const path = `/api/performance/${action.value}`
    if (action.value === 'disk_cache') await api.delete(path)
    else await api.post(path)
    action.value = null
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}
watch(
  () => auth.isRoot,
  (allowed) => {
    if (allowed) void load()
    else {
      request.cancel()
      data.value = null
      action.value = null
      loading.value = false
      error.value = ''
      updatedAt.value = null
    }
  },
  { immediate: true }
)
</script>
<template>
  <div v-if="auth.isRoot" class="space-y-6" data-ops-page>
    <PageHero :title="t('restored.overview')" :crumbs="[t('restored.runtime')]"
      ><p class="mt-2 text-sm text-[var(--text-tertiary)]">
        {{ t('restored.overviewHint') }}
      </p></PageHero
    >
    <div class="grid gap-5 lg:grid-cols-3">
      <UnavailablePanel
        v-for="key in ['health', 'quality', 'upstream']"
        :key="key"
        :title="t(`restored.${key}`)"
        :hint="t('restored.monitorHint')"
      />
    </div>
    <ErrorBanner v-if="error" :message="error" />
    <div class="flex flex-wrap gap-3">
      <ConsoleButton :disabled="loading" @click="load">{{
        t('restored.refresh')
      }}</ConsoleButton
      ><ConsoleButton
        variant="secondary"
        :disabled="loading"
        @click="action = 'gc'"
        >{{ t('restored.gc') }}</ConsoleButton
      ><ConsoleButton
        variant="secondary"
        :disabled="loading"
        @click="action = 'disk_cache'"
        >{{ t('restored.clear') }}</ConsoleButton
      ><ConsoleButton
        variant="secondary"
        :disabled="loading"
        @click="action = 'reset_stats'"
        >{{ t('restored.reset') }}</ConsoleButton
      >
    </div>
    <p v-if="updatedAt" class="text-xs text-[var(--text-tertiary)]">
      {{
        t('restored.snapshot', {
          time: new Date(updatedAt).toLocaleTimeString(locale),
        })
      }}
    </p>
    <p v-if="error && data" class="text-sm text-[var(--text-tertiary)]">
      {{ t('restored.stale') }}
    </p>
    <div class="grid gap-5 lg:grid-cols-2">
      <UnavailablePanel
        v-for="key in ['gateway', 'resources']"
        :key="key"
        :title="t(`restored.${key}`)"
        :hint="t('restored.monitorHint')"
      />
    </div>
    <div v-if="data" class="grid gap-6 lg:grid-cols-2">
      <ConsoleCard :title="t('restored.memory')"
        ><p class="mb-4 text-xs leading-5 text-[var(--text-tertiary)]">
          {{ t('restored.runtimeHint') }}
        </p>
        <StatTileGrid :tiles="memoryTiles" :columns="2" /></ConsoleCard
      ><ConsoleCard :title="t('restored.cache')">
        <pre class="overflow-auto text-xs">{{
          JSON.stringify(
            {
              cache: data.cache_stats,
              disk: data.disk_space_info,
              files: data.disk_cache_info,
            },
            null,
            2
          )
        }}</pre></ConsoleCard
      ><ConsoleCard :title="t('restored.config')">
        <pre class="overflow-auto text-xs">{{
          JSON.stringify(data.config, null, 2)
        }}</pre>
      </ConsoleCard>
    </div>
    <OpsSystemTasksCard /><ConfirmDialog
      :open="!!action"
      :title="t('restored.runtime')"
      :message="t('restored.confirm')"
      :loading="loading"
      @confirm="execute"
      @cancel="action = null"
    />
  </div>
  <p v-else role="status">{{ t('restored.forbidden') }}</p>
</template>
