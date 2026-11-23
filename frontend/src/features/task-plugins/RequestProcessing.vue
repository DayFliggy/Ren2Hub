<script setup lang="ts">
// Upstream features/request-processing/index.tsx: the instances in the order
// they run. Every change saves the whole list at once. Ren2Hub has no
// business group, so an instance runs for all requests or for its channels.
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowDown, ArrowUp, Pencil, Play, Plus, Trash2 } from 'lucide-vue-next'

import { api } from '@/api/client'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import IconButton from '@/components/common/IconButton.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import { useToast } from '@/composables/useToast'
import { localizedText } from '@/utils/localizedText'
import {
  getTaskPluginOptions,
  listRequestPluginInstances,
  saveRequestPluginInstances,
} from './api'
import PluginIcon from './components/PluginIcon.vue'
import RequestPluginDryRunDialog from './components/RequestPluginDryRunDialog.vue'
import PluginScopeSummary from './components/PluginScopeSummary.vue'
import RequestPluginInstanceEditor from './components/RequestPluginInstanceEditor.vue'
import { pluginScopeNeedsReview } from './lib/plugin-scope'
import type { RequestPluginInstance, TaskPluginOption } from './types'

const { t, locale } = useI18n()
const toast = useToast()
const request = new AbortController()

const instances = ref<RequestPluginInstance[]>([])
const plugins = ref<TaskPluginOption[]>([])
const models = ref<string[]>([])
const loading = ref(true)
const loadError = ref('')
const listSaving = ref(false)
const editorSaving = ref(false)
const editorError = ref('')
const editing = ref<RequestPluginInstance | 'new' | null>(null)
const deleteTarget = ref<RequestPluginInstance | null>(null)
const dryRunOpen = ref(false)

const busy = computed(() => listSaving.value || editorSaving.value)
const ready = computed(() => !loading.value && !loadError.value)
const instanceCounts = computed(() => {
  const counts: Record<string, number> = {}
  for (const instance of instances.value)
    counts[instance.plugin] = (counts[instance.plugin] ?? 0) + 1
  return counts
})

const textError = (cause: unknown) =>
  cause instanceof Error ? cause.message : String(cause)

function findPlugin(key: string) {
  return plugins.value.find((item) => item.key === key)
}

function pluginName(key: string) {
  const plugin = findPlugin(key)
  if (!plugin) return key
  return localizedText(plugin.name_i18n, locale.value) || plugin.name || key
}

function instanceName(id: string) {
  return instances.value.find((item) => item.id === id)?.name || id
}

function scopeText(instance: RequestPluginInstance) {
  return instance.global
    ? t('pluginExtensions.rp.allRequests')
    : t('pluginExtensions.rp.channelsOnly')
}

async function load() {
  loadError.value = ''
  loading.value = true
  try {
    const [rows, options] = await Promise.all([
      listRequestPluginInstances(request.signal),
      getTaskPluginOptions(request.signal),
    ])
    if (request.signal.aborted) return
    instances.value = rows
    plugins.value = options.filter((item) => item.transform)
  } catch (cause) {
    if (!request.signal.aborted) loadError.value = textError(cause)
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}

// The model list only feeds suggestions in the editor; typed names still work.
async function loadModels() {
  try {
    const names = await api.get<string[]>(
      '/api/channel/models_enabled',
      undefined,
      { signal: request.signal }
    )
    if (!request.signal.aborted) models.value = names ?? []
  } catch (cause) {
    if (!request.signal.aborted) toast.error(textError(cause))
  }
}

async function saveList(next: RequestPluginInstance[]) {
  listSaving.value = true
  try {
    const rows = await saveRequestPluginInstances(next, request.signal)
    if (request.signal.aborted) return
    instances.value = rows
    deleteTarget.value = null
    toast.success(t('pluginExtensions.saved'))
  } catch (cause) {
    if (!request.signal.aborted) toast.error(textError(cause))
  } finally {
    if (!request.signal.aborted) listSaving.value = false
  }
}

function move(index: number, offset: -1 | 1) {
  const next = [...instances.value]
  const [item] = next.splice(index, 1)
  if (!item) return
  next.splice(index + offset, 0, item)
  void saveList(next)
}

function setEnabled(index: number, enabled: boolean) {
  void saveList(
    instances.value.map((item, itemIndex) =>
      itemIndex === index ? { ...item, enabled } : item
    )
  )
}

function confirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  void saveList(instances.value.filter((item) => item.id !== target.id))
}

function cancelDelete() {
  if (!listSaving.value) deleteTarget.value = null
}

function openEditor(instance: RequestPluginInstance | 'new') {
  editorError.value = ''
  editing.value = instance
}

function closeEditor() {
  if (!editorSaving.value) editing.value = null
}

async function saveInstance(instance: RequestPluginInstance) {
  const current = editing.value
  const index =
    current && current !== 'new'
      ? instances.value.findIndex((item) => item.id === current.id)
      : -1
  const next =
    index < 0
      ? [...instances.value, instance]
      : instances.value.map((item, itemIndex) =>
          itemIndex === index ? instance : item
        )
  editorError.value = ''
  editorSaving.value = true
  try {
    const rows = await saveRequestPluginInstances(next, request.signal)
    if (request.signal.aborted) return
    instances.value = rows
    editing.value = null
    toast.success(t('pluginExtensions.saved'))
  } catch (cause) {
    if (!request.signal.aborted) editorError.value = textError(cause)
  } finally {
    if (!request.signal.aborted) editorSaving.value = false
  }
}

onBeforeUnmount(() => request.abort())
void load()
void loadModels()
</script>

<template>
  <div class="min-w-0" data-request-processing>
    <div class="sibling-space-y-4">
      <!-- The order note and the list actions share the first row. -->
      <div
        class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"
        data-request-processing-toolbar
      >
        <p class="min-w-0 text-sm text-[var(--text-secondary)]">
          {{ t('pluginExtensions.rp.orderNote') }}
        </p>
        <div class="flex shrink-0 flex-wrap items-center gap-2">
          <ConsoleButton
            variant="secondary"
            :disabled="!ready"
            @click="dryRunOpen = true"
          >
            <Play :size="16" aria-hidden="true" />
            {{ t('pluginExtensions.rp.dryRun') }}
          </ConsoleButton>
          <ConsoleButton :disabled="!ready || busy" @click="openEditor('new')">
            <Plus :size="16" aria-hidden="true" />
            {{ t('pluginExtensions.rp.add') }}
          </ConsoleButton>
        </div>
      </div>
      <div
        v-if="loading"
        class="sibling-space-y-2"
        :aria-label="t('pluginExtensions.rp.title')"
        aria-busy="true"
      >
        <SkeletonBlock v-for="n in 3" :key="n" class="h-14" rounded="xl" />
      </div>
      <ErrorBanner v-else-if="loadError" :message="loadError" @retry="load" />
      <div
        v-else-if="!instances.length"
        class="rounded-xl border border-dashed border-[var(--border-strong)]"
      >
        <EmptyState
          :title="t('pluginExtensions.rp.empty')"
          :hint="t('pluginExtensions.rp.emptyHint')"
        />
      </div>
      <div
        v-else
        class="subtle-scroll overflow-x-auto rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
      >
        <table
          class="w-full min-w-[44rem] text-left text-sm"
          :aria-label="t('pluginExtensions.rp.title')"
        >
          <thead
            class="border-b border-[var(--border-subtle)] text-xs text-[var(--text-tertiary)]"
          >
            <tr>
              <th class="w-28 px-3 py-2.5 font-medium">
                {{ t('pluginExtensions.rp.order') }}
              </th>
              <th class="px-3 py-2.5 font-medium">
                {{ t('pluginExtensions.rp.name') }}
              </th>
              <th class="px-3 py-2.5 font-medium">
                {{ t('pluginExtensions.rp.plugin') }}
              </th>
              <th class="px-3 py-2.5 font-medium">
                {{ t('pluginExtensions.rp.appliesTo') }}
              </th>
              <th class="px-3 py-2.5 font-medium">
                {{ t('pluginExtensions.rp.enabled') }}
              </th>
              <th class="px-3 py-2.5 text-right font-medium">
                {{ t('pluginExtensions.rp.actions') }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(instance, index) in instances"
              :key="instance.id || index"
              class="border-t border-[var(--border-subtle)] first:border-t-0"
              data-request-plugin-row
            >
              <td class="px-3 py-2">
                <div class="flex items-center gap-1">
                  <span
                    class="w-5 text-xs tabular-nums text-[var(--text-tertiary)]"
                    >{{ index + 1 }}</span
                  >
                  <IconButton
                    :label="
                      t('pluginExtensions.rp.moveUp', { name: instance.name })
                    "
                    :disabled="busy || index === 0"
                    @click="move(index, -1)"
                  >
                    <ArrowUp :size="16" aria-hidden="true" />
                  </IconButton>
                  <IconButton
                    :label="
                      t('pluginExtensions.rp.moveDown', { name: instance.name })
                    "
                    :disabled="busy || index === instances.length - 1"
                    @click="move(index, 1)"
                  >
                    <ArrowDown :size="16" aria-hidden="true" />
                  </IconButton>
                </div>
              </td>
              <td
                class="max-w-48 break-words px-3 py-2 font-medium text-[var(--text-primary)]"
              >
                {{ instance.name }}
              </td>
              <td class="px-3 py-2">
                <div class="flex min-w-0 items-center gap-2">
                  <PluginIcon
                    :plugin="
                      findPlugin(instance.plugin) ?? { key: instance.plugin }
                    "
                    :size="18"
                  />
                  <span class="min-w-0 truncate">{{
                    pluginName(instance.plugin)
                  }}</span>
                  <span
                    v-if="!findPlugin(instance.plugin)"
                    class="shrink-0 rounded-md bg-[var(--surface-muted)] px-1.5 py-0.5 text-xs text-[var(--text-secondary)]"
                    >{{ t('pluginExtensions.rp.pluginDisabled') }}</span
                  >
                </div>
              </td>
              <td
                v-if="instance.plugin_scope"
                class="max-w-72 break-words px-3 py-2"
              >
                <div class="sibling-space-y-1.5">
                  <span>{{ t('pluginExtensions.rp.scopePlugin') }}</span>
                  <PluginScopeSummary
                    :scope="instance.plugin_scope"
                    class="text-xs"
                  />
                  <button
                    v-if="
                      pluginScopeNeedsReview(
                        instance,
                        findPlugin(instance.plugin)
                      )
                    "
                    type="button"
                    data-scope-review
                    class="inline-flex items-center gap-1.5 text-xs font-medium text-[var(--accent)] hover:underline disabled:opacity-50"
                    :disabled="busy"
                    @click="openEditor(instance)"
                  >
                    <span
                      class="rounded-md bg-[var(--status-warning-soft)] px-1.5 py-0.5 text-[var(--status-warning-text)]"
                      >{{ t('pluginExtensions.rp.scopeChanged') }}</span
                    >
                    {{ t('pluginExtensions.rp.review') }}
                  </button>
                </div>
              </td>
              <td v-else class="max-w-56 break-words px-3 py-2">
                {{ scopeText(instance) }}
                <span
                  v-if="instance.models.length"
                  class="block text-xs text-[var(--text-tertiary)]"
                  >{{
                    t('pluginExtensions.rp.modelsList', {
                      models: instance.models.join(', '),
                    })
                  }}</span
                >
              </td>
              <td class="px-3 py-2">
                <ConsoleToggle
                  :model-value="instance.enabled"
                  :disabled="busy"
                  :label="
                    t('pluginExtensions.rp.enableName', { name: instance.name })
                  "
                  @update:model-value="setEnabled(index, $event)"
                />
              </td>
              <td class="px-3 py-2">
                <div class="flex justify-end gap-1">
                  <IconButton
                    :label="
                      t('pluginExtensions.rp.editName', { name: instance.name })
                    "
                    :disabled="busy"
                    @click="openEditor(instance)"
                  >
                    <Pencil :size="16" aria-hidden="true" />
                  </IconButton>
                  <IconButton
                    tone="danger"
                    :label="
                      t('pluginExtensions.rp.deleteName', {
                        name: instance.name,
                      })
                    "
                    :disabled="busy"
                    @click="deleteTarget = instance"
                  >
                    <Trash2 :size="16" aria-hidden="true" />
                  </IconButton>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <RequestPluginInstanceEditor
      :open="editing !== null"
      :instance="editing === 'new' ? null : editing"
      :plugins="plugins"
      :instance-counts="instanceCounts"
      :models="models"
      :saving="editorSaving"
      :error="editorError"
      @close="closeEditor"
      @save="saveInstance"
    />
    <RequestPluginDryRunDialog
      :open="dryRunOpen"
      :instance-name="instanceName"
      :plugin-name="pluginName"
      @close="dryRunOpen = false"
    />
    <ConfirmDialog
      :open="deleteTarget !== null"
      :title="t('pluginExtensions.rp.deleteTitle')"
      :message="
        t('pluginExtensions.rp.deleteMessage', {
          name: deleteTarget?.name ?? '',
        })
      "
      :confirm-text="t('pluginExtensions.rp.delete')"
      :loading="listSaving"
      @confirm="confirmDelete"
      @cancel="cancelDelete"
    />
  </div>
</template>
