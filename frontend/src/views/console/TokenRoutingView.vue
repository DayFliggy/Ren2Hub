<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  GripVertical,
  Plus,
  RefreshCw,
  Save,
  Trash2,
} from 'lucide-vue-next'

import {
  tokenRoutingApi,
  type TokenRoute,
  type TokenRouteEntry,
  type TokenRoutePreview,
  type TokenRouteChannel,
} from '@/api/tokenRouting'
import { ApiError } from '@/api/types'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FormField from '@/components/common/FormField.vue'
import IconButton from '@/components/common/IconButton.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import { tokenRoutingMessages } from '@/i18n/tokenRouting'
import { tokenRoutingAccess } from '@/constants/navigation/consoleNav'
import { useAuthStore } from '@/stores/auth'
import { isValidChannelRatio } from '@/utils/channelRatio'

const { t, te } = useI18n({ useScope: 'local', messages: tokenRoutingMessages })
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const toast = useToast()
const tokenId = computed(() => Number(route.params.id))
const canRead = computed(() =>
  auth.hasPermission(
    tokenRoutingAccess.requiresPermission.resource,
    tokenRoutingAccess.requiresPermission.action
  )
)
const canWrite = computed(() => auth.hasPermission('routing', 'write'))
const data = ref<TokenRoute | null>(null)
const channels = ref<TokenRouteChannel[]>([])
const selected = ref('')
const maxRatio = ref('')
const loading = ref(false)
const saving = ref(false)
const conflict = ref(false)
const loadError = ref('')
const saveError = ref('')
const candidatesLoading = ref(false)
const candidatesError = ref('')
const draggingChannelId = ref<number | null>(null)
const request = useLatestRequest()
const saveRequest = useLatestRequest()
const previewRequest = useLatestRequest()
const previewModel = ref('')
const previewPath = ref('/v1/chat/completions')
const previewStream = ref(false)
const previewTransport = ref<'http' | 'websocket'>('http')
const preview = ref<TokenRoutePreview | null>(null)
const previewLoading = ref(false)
const previewError = ref('')
const paths = [
  '/v1/chat/completions',
  '/v1/responses',
  '/v1/messages',
  '/v1/embeddings',
  '/v1/images/generations',
  '/v1/audio/transcriptions',
]
const canEdit = computed(
  () =>
    Boolean(data.value) &&
    canWrite.value &&
    canRead.value &&
    !loading.value &&
    !saving.value &&
    !candidatesLoading.value &&
    !candidatesError.value &&
    !conflict.value
)
const validRatio = computed(() => {
  const value = maxRatio.value.trim()
  return !value || isValidChannelRatio(value)
})
const available = computed(() =>
  channels.value.filter(
    (channel) =>
      !data.value?.entries.some((entry) => entry.channel_id === channel.id)
  )
)
const channelById = computed(
  () => new Map(channels.value.map((channel) => [channel.id, channel]))
)
const enabledCount = computed(
  () => data.value?.entries.filter((entry) => entry.enabled).length ?? 0
)
const ordered = computed(() => data.value?.mode === 'ordered')
const selectedOnly = computed(() => Boolean(data.value?.selected_only))
/** Saved selected scope with no entries left: the backend rejects calls until it is reconfigured. */
const savedOrphaned = ref(false)
const allDisabled = computed(
  () =>
    selectedOnly.value &&
    Boolean(data.value?.entries.length) &&
    enabledCount.value === 0
)

function errorMessage(error: unknown): string {
  if (error instanceof ApiError && error.code === 'preview_path_unsupported')
    return t('routing.pathUnsupported')
  return error instanceof Error ? error.message : String(error)
}
function reasonLabel(reason?: string): string {
  return reason && te(`routing.reasons.${reason}`)
    ? t(`routing.reasons.${reason}`)
    : reason || t('routing.reasons.channel_unavailable')
}
function channelFor(entry: TokenRouteEntry): TokenRouteChannel | undefined {
  return channelById.value.get(entry.channel_id)
}
function invalidatePreview(): void {
  previewRequest.cancel()
  preview.value = null
  previewLoading.value = false
  previewError.value = ''
}
async function load(): Promise<void> {
  saveRequest.cancel()
  saving.value = false
  invalidatePreview()
  data.value = null
  savedOrphaned.value = false
  channels.value = []
  selected.value = ''
  maxRatio.value = ''
  draggingChannelId.value = null
  conflict.value = false
  saveError.value = ''
  candidatesLoading.value = false
  candidatesError.value = ''
  loadError.value = ''
  if (!canRead.value) return
  loading.value = true
  const result = await request.run((signal) =>
    Promise.all([
      tokenRoutingApi.get(tokenId.value, signal),
      tokenRoutingApi.channels(tokenId.value, signal),
    ])
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    loadError.value = errorMessage(result.error)
    return
  }
  ;[data.value, channels.value] = result.value
  maxRatio.value = data.value.max_channel_ratio ?? ''
  savedOrphaned.value = isOrphaned(data.value)
}
function isOrphaned(value: TokenRoute): boolean {
  return value.selected_only && value.entries.length === 0
}
function setMode(mode: TokenRoute['mode']): void {
  if (!canEdit.value || !data.value) return
  data.value.mode = mode
  // Ordered routing has no platform-wide form; it always walks the saved list.
  if (mode === 'ordered') data.value.selected_only = true
}
function setSelectedOnly(value: boolean): void {
  if (!canEdit.value || !data.value || (ordered.value && !value)) return
  data.value.selected_only = value
}
function usePlatform(): void {
  setMode('auto')
  setSelectedOnly(false)
}
function updatePositions(): void {
  data.value?.entries.forEach((entry, index) => {
    entry.position = index
  })
}
function add(): void {
  const id = Number(selected.value)
  if (
    !canEdit.value ||
    !data.value?.selected_only ||
    !available.value.some((channel) => channel.id === id && channel.eligible)
  )
    return
  data.value.entries.push({
    channel_id: id,
    enabled: true,
    position: data.value.entries.length,
  })
  selected.value = ''
}
function remove(index: number): void {
  if (!canEdit.value || !data.value?.selected_only) return
  data.value.entries.splice(index, 1)
  updatePositions()
}
function move(index: number, offset: number): void {
  const entries = data.value?.entries
  const target = index + offset
  if (
    !canEdit.value ||
    !ordered.value ||
    !entries ||
    target < 0 ||
    target >= entries.length
  )
    return
  const [entry] = entries.splice(index, 1)
  if (entry) entries.splice(target, 0, entry)
  updatePositions()
}
function startDrag(event: DragEvent, entry: TokenRouteEntry): void {
  if (!canEdit.value || !ordered.value) {
    event.preventDefault()
    return
  }
  draggingChannelId.value = entry.channel_id
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', String(entry.channel_id))
  }
}
function dropEntry(target: TokenRouteEntry): void {
  const id = draggingChannelId.value
  draggingChannelId.value = null
  const entries = data.value?.entries
  if (!canEdit.value || id === null || !entries) return
  const from = entries.findIndex((entry) => entry.channel_id === id)
  const to = entries.indexOf(target)
  if (from < 0 || to < 0 || from === to) return
  move(from, to - from)
}
async function save(): Promise<void> {
  if (!canEdit.value || !data.value) return
  if (!validRatio.value) {
    saveError.value = t('routing.invalidRatio')
    return
  }
  if (data.value.selected_only && !data.value.entries.length) {
    saveError.value = t('routing.selectionRequired')
    return
  }
  // A pending request owns a snapshot, so navigation or later edits cannot alter it.
  const input: TokenRoute = {
    ...data.value,
    max_channel_ratio: maxRatio.value.trim() || null,
    entries: data.value.entries.map((entry) => ({ ...entry })),
  }
  const id = tokenId.value
  invalidatePreview()
  saveError.value = ''
  saving.value = true
  const result = await saveRequest.run(() => tokenRoutingApi.save(id, input))
  if (result.stale) return
  saving.value = false
  if (result.ok) {
    data.value = result.value
    maxRatio.value = result.value.max_channel_ratio ?? ''
    savedOrphaned.value = isOrphaned(result.value)
    toast.success(t('routing.saved'))
    selected.value = ''
    candidatesLoading.value = true
    const refreshed = await request.run((signal) =>
      tokenRoutingApi.channels(id, signal)
    )
    if (refreshed.stale) return
    candidatesLoading.value = false
    if (refreshed.ok) channels.value = refreshed.value
    else candidatesError.value = errorMessage(refreshed.error)
    return
  }
  conflict.value =
    result.error instanceof ApiError && result.error.status === 409
  saveError.value = conflict.value
    ? t('routing.conflict')
    : errorMessage(result.error)
}
async function loadPreview(): Promise<void> {
  if (
    !canRead.value ||
    !data.value ||
    loading.value ||
    saving.value ||
    !previewModel.value.trim() ||
    previewLoading.value
  )
    return
  preview.value = null
  previewLoading.value = true
  previewError.value = ''
  const result = await previewRequest.run((signal) =>
    tokenRoutingApi.preview(
      tokenId.value,
      {
        model: previewModel.value.trim(),
        path: previewPath.value,
        stream: previewStream.value,
        transport: previewTransport.value,
      },
      signal
    )
  )
  if (result.stale) return
  previewLoading.value = false
  if (result.ok) preview.value = result.value
  else previewError.value = errorMessage(result.error)
}
watch(previewTransport, (transport) => {
  if (transport === 'websocket') {
    previewPath.value = '/v1/responses'
    previewStream.value = true
  }
})
watch(
  [tokenId, previewModel, previewPath, previewStream, previewTransport],
  invalidatePreview
)
watch(canWrite, () => {
  saveRequest.cancel()
  saving.value = false
  draggingChannelId.value = null
})
watch(
  [tokenId, canRead],
  () => {
    request.cancel()
    loading.value = false
    void load()
  },
  { immediate: true }
)
</script>

<template>
  <div class="sibling-space-y-6" data-handdrawn-page="token-routing">
    <PageBreadcrumb :crumbs="[t('routing.keys'), t('routing.title')]">
      <template #action>
        <div class="flex flex-wrap gap-2">
          <ConsoleButton
            variant="secondary"
            @click="router.push({ name: 'keys' })"
          >
            <ArrowLeft class="h-4 w-4" aria-hidden="true" />{{
              t('routing.back')
            }}
          </ConsoleButton>
          <ConsoleButton
            variant="secondary"
            :disabled="loading || saving || !canRead"
            @click="load"
          >
            <RefreshCw class="h-4 w-4" aria-hidden="true" />{{
              t('routing.reload')
            }}
          </ConsoleButton>
          <ConsoleButton :loading="saving" :disabled="!canEdit" @click="save">
            <Save class="h-4 w-4" aria-hidden="true" />{{ t('routing.save') }}
          </ConsoleButton>
        </div>
      </template>
    </PageBreadcrumb>

    <header>
      <h1 class="text-2xl font-semibold text-[var(--text-primary)]">
        {{ t('routing.title') }}
      </h1>
    </header>

    <div
      v-if="saveError"
      role="alert"
      class="no-handdrawn flex flex-wrap items-center justify-between gap-4 rounded-xl border p-4 text-sm"
      :class="
        conflict
          ? 'border-[var(--status-warning)] bg-[var(--status-warning-soft)] text-[var(--status-warning-text)]'
          : 'border-[var(--status-danger)] bg-[var(--status-danger-soft)] text-[var(--status-danger-text)]'
      "
    >
      <span>{{ saveError }}</span>
      <ConsoleButton
        v-if="conflict"
        variant="secondary"
        size="sm"
        @click="load"
      >
        <RefreshCw class="h-4 w-4" aria-hidden="true" />{{
          t('routing.reload')
        }}
      </ConsoleButton>
    </div>
    <div
      v-if="candidatesError"
      role="alert"
      class="no-handdrawn flex flex-wrap items-center justify-between gap-4 rounded-xl border border-[var(--status-warning)] bg-[var(--status-warning-soft)] p-4 text-sm text-[var(--status-warning-text)]"
    >
      <span>{{
        t('routing.savedCandidatesFailed', { error: candidatesError })
      }}</span>
      <ConsoleButton variant="secondary" size="sm" @click="load">
        <RefreshCw class="h-4 w-4" aria-hidden="true" />{{
          t('routing.reload')
        }}
      </ConsoleButton>
    </div>
    <p
      v-else-if="candidatesLoading"
      role="status"
      class="text-sm text-[var(--text-tertiary)]"
    >
      {{ t('routing.refreshingCandidates') }}
    </p>
    <p
      v-if="!canRead"
      role="alert"
      class="py-16 text-center text-sm text-[var(--status-danger-text)]"
    >
      {{ t('routing.readDenied') }}
    </p>
    <div
      v-else-if="loading"
      role="status"
      class="py-16 text-center text-sm text-[var(--text-tertiary)]"
    >
      {{ t('routing.loading') }}
    </div>
    <div v-else-if="loadError" role="alert" class="py-16 text-center">
      <p class="text-sm text-[var(--status-danger-text)]">{{ loadError }}</p>
      <ConsoleButton class="mt-5" variant="secondary" @click="load"
        ><RefreshCw class="h-4 w-4" aria-hidden="true" />{{
          t('routing.reload')
        }}</ConsoleButton
      >
    </div>
    <template v-else-if="data">
      <div
        v-if="savedOrphaned"
        role="alert"
        data-route-orphaned
        class="no-handdrawn flex flex-wrap items-center justify-between gap-4 rounded-xl border border-[var(--status-warning)] bg-[var(--status-warning-soft)] p-4 text-sm text-[var(--status-warning-text)]"
      >
        <span class="min-w-0 flex-1 basis-64">{{ t('routing.orphaned') }}</span>
        <ConsoleButton
          v-if="canWrite"
          variant="secondary"
          size="sm"
          :disabled="!canEdit"
          @click="usePlatform"
          >{{ t('routing.usePlatform') }}</ConsoleButton
        >
      </div>
      <ConsoleCard :title="t('routing.modeTitle')">
        <template #action
          ><StatusChip tone="info">{{
            t('routing.keyNumber', { id: tokenId })
          }}</StatusChip></template
        >
        <fieldset class="grid gap-3 sm:grid-cols-2" :disabled="!canEdit">
          <legend class="sr-only">{{ t('routing.modeTitle') }}</legend>
          <label
            v-for="mode in ['ordered', 'auto'] as const"
            :key="mode"
            class="no-handdrawn flex min-w-0 items-start gap-3 rounded-xl border p-4 transition-colors"
            :class="
              data.mode === mode
                ? 'border-[var(--accent)] bg-[var(--accent-soft)]'
                : 'border-[var(--border-subtle)]'
            "
          >
            <input
              class="mt-1 shrink-0 accent-[var(--accent)]"
              type="radio"
              name="token-route-mode"
              :value="mode"
              :checked="data.mode === mode"
              :aria-label="t(`routing.modes.${mode}`)"
              @change="setMode(mode)"
            />
            <span class="min-w-0"
              ><span
                class="block text-sm font-semibold text-[var(--text-primary)]"
                >{{ t(`routing.modes.${mode}`) }}</span
              ><span
                class="mt-1 block text-xs leading-relaxed text-[var(--text-tertiary)]"
                >{{ t(`routing.modeHints.${mode}`) }}</span
              ></span
            >
          </label>
        </fieldset>
        <div class="mt-5 border-t border-[var(--border-subtle)] pt-4">
          <fieldset :disabled="!canEdit">
            <legend
              class="mb-3 text-xs font-semibold uppercase tracking-wide text-[var(--text-tertiary)]"
            >
              {{ t('routing.scopeTitle') }}
            </legend>
            <div class="grid gap-3 sm:grid-cols-2">
              <label
                v-for="scope in ['platform', 'selected'] as const"
                :key="scope"
                class="no-handdrawn flex min-w-0 items-start gap-3 rounded-xl border p-4 transition-colors"
                :class="[
                  selectedOnly === (scope === 'selected')
                    ? 'border-[var(--accent)] bg-[var(--accent-soft)]'
                    : 'border-[var(--border-subtle)]',
                  scope === 'platform' && ordered ? 'opacity-60' : '',
                ]"
              >
                <input
                  class="mt-1 shrink-0 accent-[var(--accent)]"
                  type="radio"
                  name="token-route-scope"
                  :value="scope"
                  :checked="selectedOnly === (scope === 'selected')"
                  :disabled="scope === 'platform' && ordered"
                  :aria-label="t(`routing.scopes.${scope}`)"
                  @change="setSelectedOnly(scope === 'selected')"
                />
                <span class="min-w-0"
                  ><span
                    class="block text-sm font-semibold text-[var(--text-primary)]"
                    >{{ t(`routing.scopes.${scope}`) }}</span
                  ><span
                    class="mt-1 block text-xs leading-relaxed text-[var(--text-tertiary)]"
                    >{{
                      scope === 'platform' && ordered
                        ? t('routing.scopeOrderedOnly')
                        : t(`routing.scopeHints.${scope}`)
                    }}</span
                  ></span
                >
              </label>
            </div>
          </fieldset>
        </div>
        <p v-if="!canWrite" class="mt-4 text-sm text-[var(--text-tertiary)]">
          {{ t('routing.readOnly') }}
        </p>
      </ConsoleCard>

      <ConsoleCard
        :title="
          !selectedOnly
            ? t('routing.scopes.platform')
            : ordered
              ? t('routing.entriesTitle')
              : t('routing.selectedTitle')
        "
      >
        <template #action
          ><StatusChip
            :tone="selectedOnly && !enabledCount ? 'warning' : 'neutral'"
            >{{
              selectedOnly
                ? t('routing.selectedCount', { count: enabledCount })
                : t('routing.scopes.platform')
            }}</StatusChip
          ></template
        >
        <div
          v-if="selectedOnly"
          class="mb-4 grid gap-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-end"
        >
          <FormField :label="t('routing.add')">
            <select
              v-model="selected"
              :aria-label="t('routing.select')"
              class="console-input h-11 w-full sm:h-10"
              :disabled="!canEdit"
            >
              <option value="">{{ t('routing.select') }}</option>
              <option
                v-for="channel in available"
                :key="channel.id"
                :value="String(channel.id)"
                :disabled="!channel.eligible"
              >
                #{{ channel.id }} {{ channel.name }} · ×{{
                  channel.channel_ratio
                }}{{
                  channel.eligible
                    ? ''
                    : ` · ${reasonLabel(channel.filter_reason)}`
                }}
              </option>
            </select>
            <p
              v-if="!channels.length"
              class="mt-2 text-xs text-[var(--text-tertiary)]"
            >
              {{ t('routing.noChannels') }}
            </p>
          </FormField>
          <ConsoleButton
            variant="secondary"
            :disabled="!canEdit || !selected"
            @click="add"
            ><Plus class="h-4 w-4" aria-hidden="true" />{{
              t('routing.add')
            }}</ConsoleButton
          >
        </div>

        <div
          class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_17.5rem]"
          :class="
            selectedOnly ? 'border-t border-[var(--border-subtle)] pt-4' : ''
          "
        >
          <div class="min-w-0">
            <template v-if="selectedOnly">
              <p
                class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--text-tertiary)]"
              >
                {{
                  ordered ? t('routing.orderHint') : t('routing.autoListHint')
                }}
              </p>
              <p
                v-if="allDisabled"
                role="alert"
                class="no-handdrawn mb-3 rounded-xl border border-[var(--status-warning)] bg-[var(--status-warning-soft)] p-3 text-sm text-[var(--status-warning-text)]"
              >
                {{ t('routing.allDisabled') }}
              </p>
              <EmptyState
                v-if="!data.entries.length"
                :title="t('routing.empty')"
                :hint="t('routing.emptyHint')"
              />
              <component
                :is="ordered ? 'ol' : 'ul'"
                v-else
                class="sibling-space-y-2"
                :aria-label="
                  ordered
                    ? t('routing.entriesTitle')
                    : t('routing.selectedTitle')
                "
              >
                <li
                  v-for="(entry, index) in data.entries"
                  :key="entry.channel_id"
                  :draggable="canEdit && ordered"
                  :data-channel-id="entry.channel_id"
                  class="flex flex-wrap items-center gap-2 rounded-xl border border-[var(--border-subtle)] p-3"
                  :class="
                    draggingChannelId === entry.channel_id ? 'opacity-50' : ''
                  "
                  @dragstart="startDrag($event, entry)"
                  @dragend="draggingChannelId = null"
                  @dragover.prevent
                  @drop.prevent="dropEntry(entry)"
                >
                  <template v-if="ordered">
                    <GripVertical
                      class="h-4 w-4 shrink-0 text-[var(--text-tertiary)]"
                      aria-hidden="true"
                    />
                    <span
                      class="text-xs tabular-nums text-[var(--text-tertiary)]"
                      >{{ index + 1 }}</span
                    >
                  </template>
                  <div class="min-w-0 basis-36 flex-1">
                    <p
                      class="break-words text-sm font-medium text-[var(--text-primary)]"
                    >
                      #{{ entry.channel_id }}
                      {{
                        channelFor(entry)?.name ||
                        t('routing.unavailable', { id: entry.channel_id })
                      }}
                    </p>
                    <div
                      class="mt-1 flex flex-wrap items-center gap-2 text-xs text-[var(--text-tertiary)]"
                    >
                      <StatusChip
                        :tone="
                          channelFor(entry)?.eligible ? 'success' : 'warning'
                        "
                        >{{
                          channelFor(entry)?.eligible
                            ? t('routing.reasons.eligible')
                            : reasonLabel(channelFor(entry)?.filter_reason)
                        }}</StatusChip
                      >
                      <span v-if="channelFor(entry)">{{
                        t('routing.channelMetrics', {
                          ratio: channelFor(entry)!.channel_ratio,
                          limit: channelFor(entry)!.max_concurrency || '∞',
                        })
                      }}</span>
                    </div>
                  </div>
                  <div class="flex flex-wrap items-center gap-1">
                    <label
                      class="mr-2 flex items-center gap-2 text-xs text-[var(--text-secondary)]"
                      ><ConsoleToggle
                        v-model="entry.enabled"
                        :label="t('routing.enabled')"
                        size="sm"
                        :disabled="!canEdit"
                      />{{ t('routing.enabled') }}</label
                    >
                    <template v-if="ordered">
                      <IconButton
                        :label="t('routing.up')"
                        :disabled="!canEdit || index === 0"
                        @click="move(index, -1)"
                        ><ArrowUp class="h-4 w-4" aria-hidden="true"
                      /></IconButton>
                      <IconButton
                        :label="t('routing.down')"
                        :disabled="
                          !canEdit || index === data.entries.length - 1
                        "
                        @click="move(index, 1)"
                        ><ArrowDown class="h-4 w-4" aria-hidden="true"
                      /></IconButton>
                    </template>
                    <IconButton
                      :label="t('routing.remove')"
                      tone="danger"
                      :disabled="!canEdit"
                      @click="remove(index)"
                      ><Trash2 class="h-4 w-4" aria-hidden="true"
                    /></IconButton>
                  </div>
                </li>
              </component>
            </template>
            <div v-else class="sibling-space-y-3">
              <p
                class="rounded-xl border border-dashed border-[var(--border-subtle)] p-4 text-sm text-[var(--text-tertiary)]"
              >
                {{ t('routing.platformHint') }}
              </p>
              <p
                v-if="data.entries.length"
                class="text-sm text-[var(--text-tertiary)]"
              >
                {{ t('routing.platformClearsList') }}
              </p>
            </div>
          </div>
          <aside
            class="sibling-space-y-3 rounded-xl bg-[var(--surface-muted)] p-4"
          >
            <p
              class="text-xs font-semibold uppercase tracking-wide text-[var(--text-tertiary)]"
            >
              {{ t('routing.policyTitle') }}
            </p>
            <FormField
              :label="t('routing.maxRatio')"
              :hint="t('routing.ratioHint')"
            >
              <TextInput
                v-model="maxRatio"
                inputmode="decimal"
                maxlength="24"
                :aria-label="t('routing.maxRatio')"
                :aria-invalid="!validRatio"
                :placeholder="t('routing.unlimitedRatio')"
                :disabled="!canEdit"
              />
            </FormField>
            <p
              v-if="!validRatio"
              role="alert"
              class="text-xs text-[var(--status-danger-text)]"
            >
              {{ t('routing.invalidRatio') }}
            </p>
          </aside>
        </div>
      </ConsoleCard>

      <ConsoleCard :title="t('routing.preview')">
        <p class="mb-4 text-sm text-[var(--text-tertiary)]">
          {{ t('routing.previewHint') }}
        </p>
        <div class="grid gap-4 md:grid-cols-2">
          <FormField :label="t('routing.model')"
            ><TextInput
              v-model="previewModel"
              :aria-label="t('routing.model')"
              :placeholder="t('routing.model')"
              :disabled="loading || saving"
          /></FormField>
          <FormField :label="t('routing.path')"
            ><select
              v-model="previewPath"
              class="console-input h-11 w-full"
              :aria-label="t('routing.path')"
              :disabled="loading || saving"
            >
              <option
                v-for="path in previewTransport === 'websocket'
                  ? ['/v1/responses']
                  : paths"
                :key="path"
                :value="path"
              >
                {{ path }}
              </option>
            </select></FormField
          >
          <FormField :label="t('routing.transport')">
            <select
              v-model="previewTransport"
              :aria-label="t('routing.transport')"
              class="console-input h-11 w-full"
              :disabled="loading || saving"
            >
              <option value="http">HTTP</option>
              <option value="websocket">WebSocket · /v1/responses</option>
            </select>
          </FormField>
        </div>
        <label
          class="mt-4 flex w-fit items-center gap-2 text-sm text-[var(--text-secondary)]"
          ><ConsoleToggle
            v-model="previewStream"
            :label="t('routing.stream')"
            size="sm"
            :disabled="loading || saving || previewTransport === 'websocket'"
          />{{ t('routing.stream') }}</label
        >
        <ConsoleButton
          class="mt-4"
          variant="secondary"
          :loading="previewLoading"
          :disabled="!previewModel.trim() || previewLoading || saving"
          @click="loadPreview"
          ><RefreshCw class="h-4 w-4" aria-hidden="true" />{{
            t('routing.preview')
          }}</ConsoleButton
        >
        <p
          v-if="previewError"
          role="alert"
          class="mt-4 text-sm text-[var(--status-danger-text)]"
        >
          {{ previewError }}
        </p>
        <div
          v-if="preview"
          class="mt-6 sibling-space-y-4 border-t border-[var(--border-subtle)] pt-4"
        >
          <div class="flex flex-wrap items-center gap-2 text-sm">
            <span class="text-[var(--text-secondary)]">{{
              t('routing.previewVersion', { version: preview.version })
            }}</span
            ><StatusChip tone="info">{{
              t(`routing.modes.${preview.mode}`)
            }}</StatusChip
            ><span class="break-all text-xs text-[var(--text-tertiary)]"
              >{{ preview.model }} · {{ preview.path }} ·
              {{ preview.transport === 'websocket' ? 'WebSocket' : 'HTTP' }} ·
              {{
                preview.stream
                  ? t('routing.streaming')
                  : t('routing.nonStreaming')
              }}</span
            >
          </div>
          <p class="text-sm text-[var(--text-tertiary)]">
            {{
              preview.ordering === 'ordered'
                ? t('routing.orderingOrdered')
                : t('routing.orderingWeighted')
            }}
          </p>
          <EmptyState
            v-if="!preview.entries.length"
            :title="t('routing.emptyPreview')"
          />
          <div v-else class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            <div
              v-for="item in preview.entries"
              :key="item.channel_id"
              class="min-w-0 rounded-xl border border-[var(--border-subtle)] p-4"
            >
              <p
                class="break-words text-sm font-medium text-[var(--text-primary)]"
              >
                #{{ item.channel_id }} {{ item.name }}
              </p>
              <StatusChip
                class="mt-2"
                :tone="item.eligible ? 'success' : 'warning'"
                >{{ reasonLabel(item.reason) }}</StatusChip
              >
              <p
                v-if="item.channel_ratio"
                class="mt-2 text-xs text-[var(--text-tertiary)]"
              >
                {{ t('routing.previewMetrics', { ratio: item.channel_ratio }) }}
              </p>
            </div>
          </div>
        </div>
      </ConsoleCard>
    </template>
  </div>
</template>
