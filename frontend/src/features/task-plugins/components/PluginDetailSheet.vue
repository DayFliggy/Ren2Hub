<script setup lang="ts">
// Upstream components/plugin-detail-sheet.tsx on ConsoleModal's drawer. Tabs:
// overview, billing parameters, source, version history, source diff, sandbox.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RotateCcw, X } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleTabs from '@/components/common/ConsoleTabs.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { useToast } from '@/composables/useToast'
import { localizedText } from '@/utils/localizedText'

import {
  activateTaskPlugin,
  getTaskPlugin,
  getTaskPluginVersions,
} from '../api'
import type {
  TaskPluginDetail,
  TaskPluginListItem,
  TaskPluginRecord,
} from '../types'
import CopyButton from './CopyButton.vue'
import JavaScriptViewer from './JavaScriptViewer.vue'
import PluginIcon from './PluginIcon.vue'
import PluginMetadataCard from './PluginMetadataCard.vue'
import PluginModelList from './PluginModelList.vue'
import PluginSandbox from './PluginSandbox.vue'
import SourceDiff from './SourceDiff.vue'
import UsageSchemaTable from './UsageSchemaTable.vue'

const props = defineProps<{ plugin: TaskPluginListItem }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { t, locale } = useI18n()
const toast = useToast()
const key = props.plugin.meta.key
const tab = ref('overview')
const sandboxVisited = ref(false)
const detail = ref<TaskPluginDetail | null>(null)
const detailError = ref('')
const versions = ref<TaskPluginRecord[] | null>(null)
const versionsError = ref('')
const compareVersion = ref('')
const compare = ref<TaskPluginDetail | null>(null)
const compareError = ref('')
const activating = ref(false)
const abort = new AbortController()

const errorText = (error: unknown) =>
  error instanceof Error ? error.message : String(error)

async function loadDetail() {
  detailError.value = ''
  try {
    detail.value = await getTaskPlugin(key, undefined, abort.signal)
  } catch (error) {
    if (!abort.signal.aborted) detailError.value = errorText(error)
  }
}
async function loadVersions() {
  versionsError.value = ''
  try {
    versions.value = await getTaskPluginVersions(key, abort.signal)
  } catch (error) {
    if (!abort.signal.aborted) versionsError.value = errorText(error)
  }
}
async function loadCompare(version: string) {
  compare.value = null
  compareError.value = ''
  if (!version) return
  try {
    const result = await getTaskPlugin(key, version, abort.signal)
    if (compareVersion.value === version) compare.value = result
  } catch (error) {
    if (!abort.signal.aborted && compareVersion.value === version)
      compareError.value = errorText(error)
  }
}
watch(compareVersion, loadCompare)
watch(tab, (value) => {
  if (value === 'sandbox') sandboxVisited.value = true
})
void loadDetail()
void loadVersions()
onBeforeUnmount(() => abort.abort())

async function activate(version: string) {
  activating.value = true
  try {
    await activateTaskPlugin(key, version)
    compareVersion.value = ''
    toast.success(t('taskPlugins.versionActivated'))
    emit('changed')
    await Promise.all([loadDetail(), loadVersions()])
  } catch (error) {
    toast.error(errorText(error))
  } finally {
    activating.value = false
  }
}

const meta = computed(() => detail.value?.meta ?? props.plugin.meta)
const description = computed(() =>
  localizedText(meta.value.description, locale.value)
)
const tabs = computed(() => [
  { key: 'overview', label: t('taskPlugins.overview') },
  { key: 'billing', label: t('taskPlugins.billingParams') },
  { key: 'source', label: t('taskPlugins.pluginSource') },
  { key: 'versions', label: t('taskPlugins.versionHistory') },
  { key: 'diff', label: t('taskPlugins.sourceDiff') },
  { key: 'sandbox', label: t('taskPlugins.pluginSandbox') },
])
const usageProfiles = computed(() => detail.value?.meta.usageProfiles ?? [])
const defaultUsageModels = computed(() => {
  const grouped = new Set(usageProfiles.value.flatMap((item) => item.models))
  return (detail.value?.meta.models ?? []).filter(
    (model) => !grouped.has(model)
  )
})
const showDefaultUsage = computed(
  () =>
    Object.keys(detail.value?.meta.usageSchema ?? {}).length > 0 &&
    (usageProfiles.value.length === 0 || defaultUsageModels.value.length > 0)
)
const compareOptions = computed(() =>
  (versions.value ?? [])
    .filter((version) => version.version !== detail.value?.meta.version)
    .map((version) => ({ value: version.version, label: version.version }))
)
</script>

<template>
  <ConsoleModal
    :open="true"
    :title="meta.name"
    size="xl"
    presentation="drawer"
    @close="emit('close')"
  >
    <div class="mb-2 flex justify-end">
      <ConsoleButton
        variant="ghost"
        size="sm"
        :aria-label="t('common.close')"
        @click="emit('close')"
      >
        <X :size="16" aria-hidden="true" />
        {{ t('common.close') }}
      </ConsoleButton>
    </div>
    <div class="mb-4 sibling-space-y-2">
      <div class="flex min-w-0 items-center gap-2">
        <PluginIcon
          :plugin="{
            ...meta,
            hasIcon: detail?.has_icon ?? plugin.has_icon,
          }"
          :size="32"
        />
        <span
          class="min-w-0 break-all font-mono text-xs text-[var(--text-tertiary)]"
          >{{ key }}</span
        >
        <CopyButton :value="key" :label="t('taskPlugins.copyPluginKey')" />
        <StatusChip
          tone="neutral"
          class="ml-auto max-w-[35%] break-all font-mono"
        >
          {{ meta.version }}
        </StatusChip>
      </div>
      <p
        class="max-h-24 overflow-y-auto break-words text-xs leading-relaxed text-[var(--text-secondary)]"
      >
        {{ description || t('taskPlugins.notDeclared') }}
      </p>
    </div>
    <div class="overflow-x-auto">
      <ConsoleTabs
        v-model="tab"
        :items="tabs"
        panel-id="task-plugin-detail-panel"
        density="compact"
        class="mb-4 min-w-max"
        :aria-label="t('taskPlugins.pluginDetails')"
      />
    </div>
    <div
      id="task-plugin-detail-panel"
      role="tabpanel"
      :aria-labelledby="`task-plugin-detail-panel-tab-${tab}`"
      class="min-w-0"
    >
      <template v-if="tab !== 'versions' && tab !== 'sandbox'">
        <div
          v-if="!detail && !detailError"
          role="status"
          class="sibling-space-y-3"
        >
          <SkeletonBlock v-for="i in 4" :key="i" rounded="lg" class="h-12" />
        </div>
        <ErrorBanner
          v-else-if="detailError"
          :message="detailError"
          @retry="loadDetail"
        />
      </template>
      <template v-if="detail">
        <PluginMetadataCard v-if="tab === 'overview'" :meta="detail.meta" />
        <div v-else-if="tab === 'billing'" class="sibling-space-y-6">
          <section
            v-if="showDefaultUsage && detail.meta.usageSchema"
            class="sibling-space-y-3"
            :aria-label="t('taskPlugins.default')"
          >
            <template v-if="usageProfiles.length">
              <h3 class="text-sm font-medium">
                {{ t('taskPlugins.default') }}
              </h3>
              <PluginModelList :models="defaultUsageModels" />
            </template>
            <UsageSchemaTable :schema="detail.meta.usageSchema" />
          </section>
          <section
            v-for="profile in usageProfiles"
            :key="profile.models[0]"
            class="sibling-space-y-3"
            :aria-label="profile.models.join(', ')"
          >
            <PluginModelList :models="profile.models" />
            <UsageSchemaTable :schema="profile.schema" />
          </section>
          <EmptyState
            v-if="!showDefaultUsage && !usageProfiles.length"
            :title="t('taskPlugins.noBilling')"
          />
        </div>
        <template v-else-if="tab === 'source'">
          <JavaScriptViewer
            v-if="detail.source"
            :value="detail.source"
            :label="t('taskPlugins.pluginSource')"
            class="max-h-[60vh]"
          />
          <EmptyState v-else :title="t('taskPlugins.noSource')" />
        </template>
        <div v-else-if="tab === 'diff'" class="sibling-space-y-3">
          <ErrorBanner
            v-if="versionsError"
            :message="versionsError"
            @retry="loadVersions"
          />
          <template v-else>
            <FilterSelect
              v-model="compareVersion"
              :options="compareOptions"
              :label="t('taskPlugins.selectCompare')"
              :placeholder="t('taskPlugins.selectCompare')"
            />
            <EmptyState
              v-if="!compareVersion"
              :title="t('taskPlugins.selectCompare')"
            />
            <ErrorBanner
              v-else-if="compareError"
              :message="compareError"
              @retry="loadCompare(compareVersion)"
            />
            <SkeletonBlock v-else-if="!compare" rounded="lg" class="h-24" />
            <SourceDiff
              v-else
              :before="compare.source"
              :after="detail.source"
            />
          </template>
        </div>
      </template>
      <template v-if="tab === 'versions'">
        <ErrorBanner
          v-if="versionsError"
          :message="versionsError"
          @retry="loadVersions"
        />
        <SkeletonBlock v-else-if="!versions" rounded="lg" class="h-24" />
        <EmptyState
          v-else-if="!versions.length"
          :title="t('taskPlugins.noVersionHistory')"
        />
        <div v-else class="overflow-x-auto">
          <table class="w-full min-w-[32rem] text-left text-sm">
            <thead class="text-xs text-[var(--text-tertiary)]">
              <tr class="border-b border-[var(--border-subtle)]">
                <th class="py-2 pr-3 font-medium">
                  {{ t('taskPlugins.version') }}
                </th>
                <th class="py-2 pr-3 font-medium">
                  {{ t('taskPlugins.remark') }}
                </th>
                <th class="py-2 pr-3 font-medium">
                  {{ t('taskPlugins.status') }}
                </th>
                <th class="py-2 text-right font-medium">
                  {{ t('taskPlugins.actions') }}
                </th>
              </tr>
            </thead>
            <tbody
              class="sibling-divide-y sibling-divide-[var(--border-subtle)]"
            >
              <tr v-for="version in versions" :key="version.id">
                <td class="py-2 pr-3 font-mono text-xs">
                  {{ version.version }}
                </td>
                <td class="max-w-64 break-words py-2 pr-3">
                  {{ version.remark || '—' }}
                </td>
                <td class="py-2 pr-3">
                  <StatusChip v-if="version.active" tone="accent">{{
                    t('taskPlugins.active')
                  }}</StatusChip>
                  <span v-else>—</span>
                </td>
                <td class="py-2 text-right">
                  <!-- Ren2Hub: factory rows are re-activated by the system, not here. -->
                  <ConsoleButton
                    size="sm"
                    variant="secondary"
                    :disabled="version.active || version.factory || activating"
                    @click="activate(version.version)"
                  >
                    <RotateCcw :size="14" aria-hidden="true" />
                    {{ t('taskPlugins.activateRollback') }}
                  </ConsoleButton>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
      <PluginSandbox
        v-if="sandboxVisited"
        v-show="tab === 'sandbox'"
        :plugin-key="key"
      />
    </div>
  </ConsoleModal>
</template>
