<script setup lang="ts">
// Upstream web/src/features/task-plugins/index.tsx: master switch with help,
// upload entry, installed / marketplace tabs, detail sheet, upload dialog.
// The page stays usable when the master switch is off.
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { onClickOutside } from '@vueuse/core'
import { CircleHelp, Upload } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleTabs from '@/components/common/ConsoleTabs.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import IconButton from '@/components/common/IconButton.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import { useToast } from '@/composables/useToast'

import {
  getTaskPluginEnabledOption,
  listTaskPlugins,
  setTaskPluginEnabledOption,
} from './api'
import MarketplacePanel from './components/MarketplacePanel.vue'
import PluginConfirmDialog from './components/PluginConfirmDialog.vue'
import PluginDetailSheet from './components/PluginDetailSheet.vue'
import PluginsTable from './components/PluginsTable.vue'
import UploadDialog from './components/UploadDialog.vue'
import type { TaskPluginListItem } from './types'

const { t } = useI18n()
const toast = useToast()
const tab = ref('installed')
const tabs = computed(() => [
  { key: 'installed', label: t('taskPlugins.installed') },
  { key: 'marketplace', label: t('taskPlugins.marketplace') },
])

const plugins = ref<TaskPluginListItem[]>([])
const pluginsLoading = ref(true)
const pluginsError = ref('')
const enabled = ref<boolean | null>(null)
const enabledError = ref('')
const enabledSaving = ref(false)
const confirmDisable = ref(false)
const helpOpen = ref(false)
const help = ref<HTMLElement | null>(null)
const detail = ref<TaskPluginListItem | null>(null)
const upload = ref<{ key: string } | null>(null)
let listSequence = 0

const errorText = (error: unknown) =>
  error instanceof Error ? error.message : String(error)

async function loadPlugins() {
  const sequence = ++listSequence
  pluginsError.value = ''
  try {
    const result = await listTaskPlugins()
    if (sequence === listSequence) plugins.value = result
  } catch (error) {
    if (sequence === listSequence) pluginsError.value = errorText(error)
  } finally {
    if (sequence === listSequence) pluginsLoading.value = false
  }
}

// The switch loads on its own so an option failure never hides the plugins.
async function loadEnabled() {
  enabledError.value = ''
  try {
    enabled.value = await getTaskPluginEnabledOption()
  } catch (error) {
    enabledError.value = errorText(error)
  }
}

async function saveEnabled(value: boolean) {
  enabledSaving.value = true
  try {
    await setTaskPluginEnabledOption(value)
    enabled.value = value
    confirmDisable.value = false
    toast.success(t('taskPlugins.settingUpdated'))
  } catch (error) {
    toast.error(errorText(error))
  } finally {
    enabledSaving.value = false
  }
}

function toggleEnabled(value: boolean) {
  if (value) void saveEnabled(true)
  else confirmDisable.value = true
}

onClickOutside(help, () => (helpOpen.value = false))
void loadPlugins()
void loadEnabled()

const overrides = computed(() =>
  plugins.value.filter((plugin) => plugin.source === 'override')
)
</script>

<template>
  <div class="min-w-0">
    <PageBreadcrumb :crumbs="[t('nav.groupAdmin'), t('taskPlugins.title')]">
      <template #action>
        <div class="flex flex-wrap items-center gap-2">
          <div class="flex items-center gap-2" data-task-plugin-master>
            <ConsoleToggle
              id="task-plugin-enabled-switch"
              :model-value="enabled ?? false"
              :disabled="enabled === null || enabledSaving"
              :loading="enabledSaving"
              :label="t('taskPlugins.enableTaskPlugins')"
              @update:model-value="toggleEnabled"
            />
            <label
              for="task-plugin-enabled-switch"
              class="hidden text-sm text-[var(--text-secondary)] sm:inline"
              >{{ t('taskPlugins.enableTaskPlugins') }}</label
            >
            <div ref="help" class="relative" @keydown.escape="helpOpen = false">
              <IconButton
                :label="t('taskPlugins.behaviorHelp')"
                :aria-expanded="helpOpen"
                aria-controls="task-plugin-help"
                @click="helpOpen = !helpOpen"
              >
                <CircleHelp :size="16" aria-hidden="true" />
              </IconButton>
              <div
                v-if="helpOpen"
                id="task-plugin-help"
                role="dialog"
                :aria-label="t('taskPlugins.behaviorHelp')"
                class="absolute right-0 top-full z-30 mt-2 w-80 max-w-[calc(100vw-2rem)] sibling-space-y-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-4 text-sm shadow-lg"
              >
                <p class="font-medium">
                  {{ t('taskPlugins.enableTaskPlugins') }}
                </p>
                <p class="text-[var(--text-secondary)]">
                  {{ t('taskPlugins.masterSwitchHelp') }}
                </p>
                <p class="font-medium">{{ t('taskPlugins.behaviorHelp') }}</p>
                <p class="text-[var(--text-secondary)]">
                  {{ t('taskPlugins.factoryBehaviorHelp') }}
                </p>
              </div>
            </div>
          </div>
          <ConsoleButton
            v-if="tab === 'installed'"
            size="sm"
            @click="upload = { key: '' }"
          >
            <Upload :size="14" aria-hidden="true" />
            {{ t('taskPlugins.uploadPlugin') }}
          </ConsoleButton>
        </div>
      </template>
    </PageBreadcrumb>
    <ErrorBanner
      v-if="enabledError"
      class="mb-3"
      :message="enabledError"
      @retry="loadEnabled"
    />
    <ConsoleTabs
      v-model="tab"
      :items="tabs"
      panel-id="task-plugins-panel"
      class="mb-4"
      :aria-label="t('taskPlugins.sections')"
    />
    <div
      id="task-plugins-panel"
      role="tabpanel"
      :aria-labelledby="`task-plugins-panel-tab-${tab}`"
    >
      <template v-if="tab === 'installed'">
        <ErrorBanner
          v-if="pluginsError"
          class="mb-3"
          :message="pluginsError"
          @retry="loadPlugins"
        />
        <PluginsTable
          :plugins="plugins"
          :loading="pluginsLoading"
          @details="detail = $event"
          @upload="upload = { key: $event }"
          @changed="loadPlugins"
        />
      </template>
      <MarketplacePanel v-else :installed="plugins" @changed="loadPlugins" />
    </div>
    <PluginDetailSheet
      v-if="detail"
      :key="detail.meta.key"
      :plugin="detail"
      @close="detail = null"
      @changed="loadPlugins"
    />
    <UploadDialog
      v-if="upload"
      :initial-key="upload.key"
      @close="upload = null"
      @changed="loadPlugins"
    />
    <PluginConfirmDialog
      :open="confirmDisable"
      :title="t('taskPlugins.disableMasterTitle')"
      :message="t('taskPlugins.disableMasterDesc')"
      destructive
      :loading="enabledSaving"
      :confirm-text="t('taskPlugins.disable')"
      @cancel="!enabledSaving && (confirmDisable = false)"
      @confirm="saveEnabled(false)"
    >
      <ul v-if="overrides.length" class="list-disc pl-5">
        <li v-for="plugin in overrides" :key="plugin.meta.key">
          {{ plugin.meta.name }} ({{ plugin.meta.key }}):
          {{
            t('taskPlugins.pluginUsageLine', {
              channels: plugin.channel_count,
              tasks: plugin.in_flight_count,
            })
          }}
        </li>
      </ul>
    </PluginConfirmDialog>
  </div>
</template>
