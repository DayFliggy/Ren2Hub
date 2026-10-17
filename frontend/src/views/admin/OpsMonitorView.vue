<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import OpsSystemTasksCard from '@/components/admin/ops/OpsSystemTasksCard.vue'
const { t } = useI18n({
  messages: {
    'zh-CN': {
      runtime: {
        title: '运行状态',
        memory: 'Go 运行时内存',
        cache: '缓存与磁盘',
        config: '性能配置',
        refresh: '刷新',
        gc: '执行垃圾回收',
        clear: '清理不活跃缓存',
        reset: '重置统计',
        confirm: '确认执行此管理操作？',
      },
    },
    en: {
      runtime: {
        title: 'Runtime status',
        memory: 'Go runtime memory',
        cache: 'Cache and disk',
        config: 'Performance configuration',
        refresh: 'Refresh',
        gc: 'Run garbage collection',
        clear: 'Clean inactive cache',
        reset: 'Reset statistics',
        confirm: 'Run this administration action?',
      },
    },
  },
})
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
async function load() {
  loading.value = true
  error.value = ''
  try {
    data.value = await api.get('/api/performance/stats')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}
async function execute() {
  if (!action.value) return
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
onMounted(load)
</script>
<template>
  <div class="space-y-6">
    <PageBreadcrumb :crumbs="[t('runtime.title')]" /><ErrorBanner
      v-if="error"
      :message="error"
    />
    <div class="flex flex-wrap gap-3">
      <ConsoleButton :disabled="loading" @click="load">{{
        t('runtime.refresh')
      }}</ConsoleButton
      ><ConsoleButton
        variant="secondary"
        :disabled="loading"
        @click="action = 'gc'"
        >{{ t('runtime.gc') }}</ConsoleButton
      ><ConsoleButton
        variant="secondary"
        :disabled="loading"
        @click="action = 'disk_cache'"
        >{{ t('runtime.clear') }}</ConsoleButton
      ><ConsoleButton
        variant="secondary"
        :disabled="loading"
        @click="action = 'reset_stats'"
        >{{ t('runtime.reset') }}</ConsoleButton
      >
    </div>
    <div v-if="data" class="grid gap-6 lg:grid-cols-2">
      <ConsoleCard :title="t('runtime.memory')"
        ><dl>
          <div
            v-for="(value, key) in data.memory_stats"
            :key="key"
            class="flex justify-between gap-4 py-2"
          >
            <dt>{{ key }}</dt>
            <dd>{{ value }}</dd>
          </div>
        </dl></ConsoleCard
      ><ConsoleCard :title="t('runtime.cache')">
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
      ><ConsoleCard :title="t('runtime.config')">
        <pre class="overflow-auto text-xs">{{
          JSON.stringify(data.config, null, 2)
        }}</pre>
      </ConsoleCard>
    </div>
    <OpsSystemTasksCard /><ConfirmDialog
      :open="!!action"
      :title="t('runtime.title')"
      :message="t('runtime.confirm')"
      :loading="loading"
      @confirm="execute"
      @cancel="action = null"
    />
  </div>
</template>
