<script setup lang="ts">
// Upstream components/plugins-table.tsx: plugin list (table or card view),
// enable switch with confirmation, row menu, delete, and the in-use dialog.
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useLocalStorage, useMediaQuery } from '@vueuse/core'
import { Eye, Trash2, Upload } from 'lucide-vue-next'

import ActionMenu, {
  type ActionMenuItem,
} from '@/components/common/ActionMenu.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import { useToast } from '@/composables/useToast'
import { localizedText } from '@/utils/localizedText'

import {
  deleteTaskPluginVersion,
  setTaskPluginStatus,
  TaskPluginUsageError,
} from '../api'
import { channelTypeLabel } from '../lib/channel-type'
import type { TaskPluginListItem, TaskPluginUsage } from '../types'
import PluginCard from './PluginCard.vue'
import PluginConfirmDialog from './PluginConfirmDialog.vue'
import PluginIcon from './PluginIcon.vue'
import PluginStatusBadges from './PluginStatusBadges.vue'
import PluginWebsiteLink from './PluginWebsiteLink.vue'
import { usePluginChangeNotice } from '../usePluginChangeNotice'

const props = defineProps<{
  plugins: TaskPluginListItem[]
  loading: boolean
}>()
const emit = defineEmits<{
  details: [plugin: TaskPluginListItem]
  upload: [key: string]
  changed: []
}>()
const { t, locale } = useI18n()
const toast = useToast()
const notifyChange = usePluginChangeNotice()

const filter = ref('')
const viewMode = useLocalStorage<'table' | 'card'>(
  'task-plugins-view-mode',
  'table'
)
const isMobile = useMediaQuery('(max-width: 639px)')
const viewOptions = computed(() => [
  { value: 'table', label: t('taskPlugins.tableView') },
  { value: 'card', label: t('taskPlugins.cardView') },
])
const columns = computed<TableColumn[]>(() => [
  { key: 'plugin', label: t('taskPlugins.colPlugin') },
  { key: 'version', label: t('taskPlugins.activeVersion'), width: '120px' },
  { key: 'source', label: t('taskPlugins.colSource'), width: '190px' },
  { key: 'channelType', label: t('taskPlugins.channelType'), width: '150px' },
  { key: 'models', label: t('taskPlugins.models'), width: '72px' },
  { key: 'enabled', label: t('taskPlugins.enabled'), width: '84px' },
  { key: 'runtime', label: t('taskPlugins.runtimeStatus'), width: '170px' },
  { key: 'actions', label: '', width: '56px', align: 'right' },
])
const rows = computed(() => {
  const query = filter.value.trim().toLowerCase()
  return props.plugins
    .filter(
      (plugin) =>
        !query ||
        `${plugin.meta.name} ${plugin.meta.key}`.toLowerCase().includes(query)
    )
    .map((plugin) => ({ key: plugin.meta.key, plugin }))
})

const busy = ref(false)
const statusConfirmation = ref<{
  plugin: TaskPluginListItem
  enabled: boolean
} | null>(null)
const statusTarget = ref<TaskPluginListItem | null>(null)
const deleteTarget = ref<TaskPluginListItem | null>(null)
const blockedUsage = ref<TaskPluginUsage | null>(null)
const blockedAction = ref<'delete' | 'disable' | null>(null)

function menuItems(plugin: TaskPluginListItem): ActionMenuItem[] {
  return [
    { key: 'details', label: t('taskPlugins.details'), icon: Eye },
    { key: 'upload', label: t('taskPlugins.uploadNewVersion'), icon: Upload },
    {
      key: 'delete',
      label: t('taskPlugins.deleteActiveCustom'),
      icon: Trash2,
      tone: 'danger',
      disabled: plugin.source === 'factory',
    },
  ]
}
function selectMenu(plugin: TaskPluginListItem, key: string) {
  if (key === 'details') emit('details', plugin)
  else if (key === 'upload') emit('upload', plugin.meta.key)
  else if (key === 'delete') deleteTarget.value = plugin
}

function closeBlocked() {
  blockedAction.value = null
  blockedUsage.value = null
}

async function setStatus(
  plugin: TaskPluginListItem,
  enabled: boolean,
  options?: { cascade?: boolean; force?: boolean }
) {
  if (busy.value) return
  busy.value = true
  statusTarget.value = plugin
  try {
    const notice = await setTaskPluginStatus(plugin.meta.key, enabled, options)
    statusConfirmation.value = null
    closeBlocked()
    emit('changed')
    if (enabled)
      notifyChange(t('taskPlugins.pluginEnabled'), notice?.pricing_issues)
  } catch (error) {
    if (error instanceof TaskPluginUsageError) {
      statusConfirmation.value = null
      blockedUsage.value = error.usage
      blockedAction.value = 'disable'
      return
    }
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    busy.value = false
  }
}

async function deleteActive(plugin: TaskPluginListItem, force = false) {
  if (busy.value) return
  busy.value = true
  try {
    const notice = await deleteTaskPluginVersion(
      plugin.meta.key,
      plugin.meta.version,
      force
    )
    deleteTarget.value = null
    closeBlocked()
    notifyChange(t('taskPlugins.versionDeleted'), notice?.pricing_issues)
    emit('changed')
  } catch (error) {
    if (error instanceof TaskPluginUsageError) {
      blockedUsage.value = error.usage
      blockedAction.value = 'delete'
      return
    }
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    busy.value = false
  }
}

function forceBlocked() {
  if (blockedAction.value === 'delete') {
    if (deleteTarget.value) void deleteActive(deleteTarget.value, true)
    return
  }
  if (!statusTarget.value || !blockedUsage.value) return
  void setStatus(statusTarget.value, false, {
    cascade: blockedUsage.value.channels.length > 0,
    force: true,
  })
}

function channelSummary(plugin: TaskPluginListItem) {
  const types = plugin.meta.channelTypes ?? []
  if (!types.length) return { label: t('taskPlugins.taskPlugin'), ids: '' }
  return {
    label: channelTypeLabel(types[0]!, t('taskPlugins.taskPlugin')),
    ids: types.map((type) => `#${type}`).join(' '),
  }
}
</script>

<template>
  <div class="sibling-space-y-3">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <SearchInput
        v-model="filter"
        class="w-full sm:max-w-xs"
        :placeholder="t('taskPlugins.filterPlaceholder')"
      />
      <SegmentedToggle
        v-if="!isMobile"
        v-model="viewMode"
        :options="viewOptions"
        :label="t('taskPlugins.viewMode')"
        size="sm"
      />
    </div>
    <div
      v-if="loading && (isMobile || viewMode === 'card')"
      class="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-4 xl:grid-cols-3"
    >
      <SkeletonBlock v-for="i in 3" :key="i" rounded="xl" class="h-44" />
    </div>
    <template v-else-if="isMobile || viewMode === 'card'">
      <EmptyState
        v-if="!rows.length"
        :title="t('taskPlugins.emptyTitle')"
        :hint="t('taskPlugins.emptyHint')"
      />
      <div
        v-else
        class="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-4 xl:grid-cols-3"
      >
        <PluginCard
          v-for="row in rows"
          :key="row.key"
          :plugin="row.plugin"
          :menu-items="menuItems(row.plugin)"
          :busy="busy"
          @select="selectMenu(row.plugin, $event)"
          @toggle="statusConfirmation = { plugin: row.plugin, enabled: $event }"
        />
      </div>
    </template>
    <DataTable
      v-else
      :columns="columns"
      :rows="rows"
      row-key="key"
      :loading="loading"
      :skeleton-rows="4"
      :empty-title="t('taskPlugins.emptyTitle')"
      :empty-hint="t('taskPlugins.emptyHint')"
    >
      <template #cell-plugin="{ row }">
        <div
          class="flex min-w-0 items-center gap-2"
          :title="
            localizedText(row.plugin.meta.description, locale) || undefined
          "
        >
          <PluginIcon
            :plugin="{ ...row.plugin.meta, hasIcon: row.plugin.has_icon }"
            :size="18"
          />
          <div class="min-w-0">
            <div class="truncate text-sm font-medium">
              {{ row.plugin.meta.name }}
            </div>
            <div class="truncate font-mono text-xs text-[var(--text-tertiary)]">
              {{ row.plugin.meta.key }}
            </div>
            <PluginWebsiteLink :website="row.plugin.meta.website" />
          </div>
        </div>
      </template>
      <template #cell-version="{ row }">
        <span class="font-mono text-xs">{{ row.plugin.meta.version }}</span>
      </template>
      <template #cell-source="{ row }">
        <PluginStatusBadges :plugin="row.plugin" part="source" />
      </template>
      <template #cell-channelType="{ row }">
        <span class="text-xs"
          >{{ channelSummary(row.plugin).label
          }}<span
            v-if="channelSummary(row.plugin).ids"
            class="ml-1 text-[var(--text-tertiary)]"
            >{{ channelSummary(row.plugin).ids }}</span
          ></span
        >
      </template>
      <template #cell-models="{ row }">
        {{ row.plugin.meta.models?.length ?? 0 }}
      </template>
      <template #cell-enabled="{ row }">
        <ConsoleToggle
          :model-value="
            row.plugin.enabled && row.plugin.runtime_status !== 'compile_failed'
          "
          size="sm"
          :disabled="busy || row.plugin.runtime_status === 'compile_failed'"
          :label="
            t('taskPlugins.enablePluginAria', { key: row.plugin.meta.key })
          "
          @update:model-value="
            statusConfirmation = { plugin: row.plugin, enabled: $event }
          "
        />
      </template>
      <template #cell-runtime="{ row }">
        <PluginStatusBadges :plugin="row.plugin" part="runtime" />
      </template>
      <template #cell-actions="{ row }">
        <ActionMenu
          :items="menuItems(row.plugin)"
          :label="t('taskPlugins.openMenu')"
          @select="selectMenu(row.plugin, $event)"
        />
      </template>
    </DataTable>

    <PluginConfirmDialog
      :open="statusConfirmation !== null"
      :title="
        statusConfirmation?.enabled
          ? t('taskPlugins.enableTitle')
          : t('taskPlugins.disableTitle')
      "
      :message="
        t(
          statusConfirmation?.enabled
            ? 'taskPlugins.enableConfirm'
            : 'taskPlugins.disableConfirm',
          {
            name: statusConfirmation?.plugin.meta.name,
            key: statusConfirmation?.plugin.meta.key,
          }
        )
      "
      :destructive="!statusConfirmation?.enabled"
      :loading="busy"
      :confirm-text="
        statusConfirmation?.enabled
          ? t('taskPlugins.enable')
          : t('taskPlugins.disable')
      "
      @cancel="!busy && (statusConfirmation = null)"
      @confirm="
        statusConfirmation &&
        setStatus(statusConfirmation.plugin, statusConfirmation.enabled)
      "
    />
    <PluginConfirmDialog
      :open="deleteTarget !== null && blockedAction === null"
      :title="t('taskPlugins.deleteTitle')"
      :message="
        deleteTarget?.factory_meta
          ? t('taskPlugins.deleteWithFallback')
          : t('taskPlugins.deleteWithoutFallback')
      "
      destructive
      :loading="busy"
      :confirm-text="t('taskPlugins.delete')"
      @cancel="!busy && (deleteTarget = null)"
      @confirm="deleteTarget && deleteActive(deleteTarget)"
    />
    <PluginConfirmDialog
      :open="blockedAction !== null"
      :title="t('taskPlugins.inUseTitle')"
      destructive
      :loading="busy"
      :hide-confirm="
        blockedAction === 'disable' && blockedUsage?.in_flight_count === 0
      "
      :confirm-text="t('taskPlugins.forceOperation')"
      @cancel="!busy && ((deleteTarget = null), closeBlocked())"
      @confirm="forceBlocked"
    >
      <template v-if="blockedUsage">
        <p>
          {{
            t('taskPlugins.inUseSummary', {
              count: blockedUsage.channels.length,
              tasks: blockedUsage.in_flight_count,
            })
          }}
        </p>
        <ul
          v-if="blockedUsage.channels.length"
          class="list-disc sibling-space-y-1 pl-5"
        >
          <li v-for="channel in blockedUsage.channels" :key="channel.id">
            #{{ channel.id }} {{ channel.name }}
            <span
              v-if="blockedAction === 'disable' && channel.type === 60"
              class="block text-xs text-[var(--text-tertiary)]"
              >{{ t('taskPlugins.newApiUnbind') }}</span
            >
          </li>
        </ul>
        <p v-if="blockedAction === 'delete'" class="text-xs">
          {{ t('taskPlugins.deleteBlockedHint') }}
        </p>
      </template>
      <template
        v-if="blockedAction === 'disable' && blockedUsage?.channels.length"
        #actions
      >
        <ConsoleButton
          variant="secondary"
          :disabled="busy || blockedUsage.in_flight_count > 0"
          @click="
            statusTarget && setStatus(statusTarget, false, { cascade: true })
          "
        >
          {{ t('taskPlugins.cascadeDisable') }}
        </ConsoleButton>
      </template>
    </PluginConfirmDialog>
  </div>
</template>
