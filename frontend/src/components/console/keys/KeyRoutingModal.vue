<script setup lang="ts">
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Check,
  GripVertical,
  Info,
  Lock,
  RefreshCw,
  Search,
  Trash2,
} from 'lucide-vue-next'
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { getActivePinia } from 'pinia'
import { useI18n } from 'vue-i18n'

import {
  tokenRoutingApi,
  type TokenRoute,
  type TokenRouteChannel,
  type TokenRouteMode,
} from '@/api/tokenRouting'
import { ApiError } from '@/api/types'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import IconButton from '@/components/common/IconButton.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import { tokenRoutingAccess } from '@/constants/navigation/consoleNav'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import type { TokenSummary } from '@/types/console'

const props = withDefaults(
  defineProps<{
    open: boolean
    token: TokenSummary | null
    /** Optional mode intent when jumping from KeyFormModal to pick ordered channels. */
    initialMode?: TokenRouteMode | null
  }>(),
  { initialMode: null }
)

const emit = defineEmits<{
  close: []
  saved: []
  'edit-policy': [token: TokenSummary]
}>()

const { t, te } = useI18n()
const toast = useToast()

const routingFeatureOn = computed(() => {
  if (!getActivePinia()) return false
  return useAppStore().isFeatureEnabled(tokenRoutingAccess.feature)
})
const canRead = computed(() => {
  if (!routingFeatureOn.value || !getActivePinia()) return false
  return useAuthStore().hasPermission(
    tokenRoutingAccess.requiresPermission.resource,
    tokenRoutingAccess.requiresPermission.action
  )
})
const canWrite = computed(() => {
  if (!canRead.value || !getActivePinia()) return false
  return useAuthStore().hasPermission('routing', 'write')
})

const loading = ref(false)
const loadError = ref('')
const saving = ref(false)
const saveError = ref('')
const conflict = ref(false)
const revertedNote = ref(false)

const candidatesLoading = ref(false)
const candidatesError = ref('')
const channels = ref<TokenRouteChannel[]>([])

const searchQuery = ref('')
const onlyEligible = ref(false)
const sortByRatio = ref(false)
const visibleLimit = ref(20)
const draggingChannelId = ref<number | null>(null)

const routeRequest = useLatestRequest()
const candidatesRequest = useLatestRequest()

interface DraftEntry {
  channel_id: number
  enabled: boolean
}

interface NormalizedSnapshot {
  mode: TokenRouteMode
  selected_only: boolean
  entries: Array<[number, boolean]>
}

const draft = reactive({
  mode: 'auto' as TokenRouteMode,
  entries: [] as DraftEntry[],
  maxRatio: '' as string,
  orphan: false,
})

const version = ref(0)
let baseline: NormalizedSnapshot = defaultSnapshot()

function defaultSnapshot(): NormalizedSnapshot {
  return {
    mode: 'auto',
    selected_only: false,
    entries: [],
  }
}

function buildSnapshot(
  mode: TokenRouteMode,
  selectedOnly: boolean,
  entries: DraftEntry[]
): NormalizedSnapshot {
  const effectiveEntries = selectedOnly ? entries : []
  return {
    mode,
    selected_only: selectedOnly,
    entries: effectiveEntries.map((entry) => [entry.channel_id, entry.enabled]),
  }
}

function snapshotsEqual(a: NormalizedSnapshot, b: NormalizedSnapshot): boolean {
  if (
    a.mode !== b.mode ||
    a.selected_only !== b.selected_only ||
    a.entries.length !== b.entries.length
  ) {
    return false
  }
  for (let i = 0; i < a.entries.length; i++) {
    if (
      a.entries[i]![0] !== b.entries[i]![0] ||
      a.entries[i]![1] !== b.entries[i]![1]
    ) {
      return false
    }
  }
  return true
}

const modelLimits = computed(() => props.token?.model_limits ?? [])
const derivedSelectedOnly = computed(
  () => draft.entries.length > 0 || draft.orphan
)
const enabledCount = computed(
  () => draft.entries.filter((entry) => entry.enabled).length
)
const isAllPaused = computed(
  () => draft.entries.length > 0 && enabledCount.value === 0
)
const ordered = computed(() => draft.mode === 'ordered')
const pendingOrderedIntent = computed(
  () => props.initialMode === 'ordered' && baseline.mode !== 'ordered'
)

const currentSnapshot = computed(() =>
  buildSnapshot(draft.mode, derivedSelectedOnly.value, draft.entries)
)

const dirty = computed(() => !snapshotsEqual(currentSnapshot.value, baseline))

const canInteract = computed(
  () =>
    canRead.value &&
    canWrite.value &&
    !saving.value &&
    !loading.value &&
    !loadError.value &&
    !conflict.value
)

const validationError = computed(() => {
  if (!canWrite.value || !dirty.value) return ''
  if (draft.orphan) return t('keys.routingPanel.orphanedRequired')
  if (draft.mode === 'ordered' && draft.entries.length === 0) {
    return t('keys.routingPanel.orderedRequired')
  }
  return ''
})

const canSave = computed(
  () => canInteract.value && dirty.value && validationError.value === ''
)

const channelById = computed(
  () => new Map(channels.value.map((channel) => [channel.id, channel]))
)
const selectedIds = computed(
  () => new Set(draft.entries.map((entry) => entry.channel_id))
)

const filteredCandidates = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  const list = channels.value.filter((channel) => {
    if (onlyEligible.value && !channel.eligible) return false
    if (!query) return true
    return (
      channel.name.toLowerCase().includes(query) ||
      String(channel.id).includes(query) ||
      `#${channel.id}`.includes(query) ||
      channel.models.some((model) => model.toLowerCase().includes(query))
    )
  })
  if (sortByRatio.value) {
    return [...list].sort(
      (a, b) => Number(a.channel_ratio) - Number(b.channel_ratio) || a.id - b.id
    )
  }
  return list
})

const visibleChannels = computed(() =>
  filteredCandidates.value.slice(0, visibleLimit.value)
)
const remainingCount = computed(() =>
  Math.max(0, filteredCandidates.value.length - visibleChannels.value.length)
)

function reasonLabel(reason?: string): string {
  if (!reason) return t('keys.routingPanel.reasons.channel_unavailable')
  const key = `keys.routingPanel.reasons.${reason}`
  return te(key) ? t(key) : reason
}

function channelFor(entry: DraftEntry): TokenRouteChannel | undefined {
  return channelById.value.get(entry.channel_id)
}

function missingChannelText(id: number): string {
  return modelLimits.value.length > 0
    ? t('keys.routingPanel.unavailableByModels', { id })
    : t('keys.routingPanel.unavailableChannel', { id })
}

function clearErrors(): void {
  saveError.value = ''
  conflict.value = false
}

function applyRoute(route: TokenRoute): void {
  version.value = route.version
  draft.mode = route.mode
  draft.entries = route.entries.map((entry) => ({
    channel_id: entry.channel_id,
    enabled: entry.enabled,
  }))
  draft.maxRatio = route.max_channel_ratio ?? ''
  draft.orphan = route.selected_only && route.entries.length === 0
  baseline = buildSnapshot(draft.mode, route.selected_only, draft.entries)
  revertedNote.value = false
}

async function fetchCandidates(): Promise<void> {
  if (!props.open || !props.token || !canRead.value) return
  candidatesLoading.value = true
  candidatesError.value = ''
  const ratio = draft.maxRatio.trim()
  const limits = [...modelLimits.value]
  const result = await candidatesRequest.run((signal) =>
    tokenRoutingApi.candidates(
      { model_limits: limits, max_channel_ratio: ratio || undefined },
      signal
    )
  )
  if (result.stale) return
  candidatesLoading.value = false
  if (result.ok) {
    channels.value = result.value
  } else {
    candidatesError.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
  }
}

async function loadAll(): Promise<void> {
  routeRequest.cancel()
  candidatesRequest.cancel()
  clearErrors()
  searchQuery.value = ''
  onlyEligible.value = false
  sortByRatio.value = false
  visibleLimit.value = 20
  draggingChannelId.value = null
  channels.value = []
  candidatesError.value = ''
  loadError.value = ''

  if (!props.open || !props.token || !canRead.value) {
    loading.value = false
    candidatesLoading.value = false
    return
  }

  loading.value = true
  candidatesLoading.value = true
  const id = props.token.id
  const limits = [...modelLimits.value]

  const routeResult = await routeRequest.run((signal) =>
    tokenRoutingApi.get(id, signal)
  )
  if (routeResult.stale) return
  loading.value = false
  if (!routeResult.ok) {
    loadError.value =
      routeResult.error instanceof Error
        ? routeResult.error.message
        : String(routeResult.error)
    candidatesLoading.value = false
    return
  }
  applyRoute(routeResult.value)
  if (
    props.initialMode === 'ordered' &&
    canWrite.value &&
    draft.mode === 'auto' &&
    !draft.orphan
  ) {
    draft.mode = 'ordered'
  }

  const ratio = routeResult.value.max_channel_ratio ?? ''
  const candResult = await candidatesRequest.run((signal) =>
    tokenRoutingApi.candidates(
      { model_limits: limits, max_channel_ratio: ratio || undefined },
      signal
    )
  )
  if (candResult.stale) return
  candidatesLoading.value = false
  if (candResult.ok) {
    channels.value = candResult.value
  } else {
    candidatesError.value =
      candResult.error instanceof Error
        ? candResult.error.message
        : String(candResult.error)
  }
}

function usePlatform(): void {
  if (!canInteract.value) return
  clearErrors()
  draft.mode = 'auto'
  draft.entries = []
  draft.orphan = false
  revertedNote.value = true
}

function toggleCandidate(channel: TokenRouteChannel): void {
  if (!canInteract.value) return
  const index = draft.entries.findIndex(
    (entry) => entry.channel_id === channel.id
  )
  if (index >= 0) {
    removeEntry(index)
    return
  }
  if (!channel.eligible) return
  clearErrors()
  draft.orphan = false
  revertedNote.value = false
  draft.entries.push({ channel_id: channel.id, enabled: true })
}

function removeEntry(index: number): void {
  if (!canInteract.value) return
  clearErrors()
  draft.entries.splice(index, 1)
  if (draft.entries.length === 0 && draft.mode === 'auto') {
    draft.orphan = false
    revertedNote.value = true
  }
}

function moveEntry(index: number, delta: number): void {
  const target = index + delta
  if (
    !canInteract.value ||
    !ordered.value ||
    target < 0 ||
    target >= draft.entries.length
  ) {
    return
  }
  clearErrors()
  const [entry] = draft.entries.splice(index, 1)
  if (entry) draft.entries.splice(target, 0, entry)
}

function startDrag(event: DragEvent, entry: DraftEntry): void {
  if (!canInteract.value || !ordered.value) {
    event.preventDefault()
    return
  }
  draggingChannelId.value = entry.channel_id
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', String(entry.channel_id))
  }
}

function dropEntry(target: DraftEntry): void {
  const id = draggingChannelId.value
  draggingChannelId.value = null
  if (!canInteract.value || id === null) return
  const from = draft.entries.findIndex((entry) => entry.channel_id === id)
  const to = draft.entries.indexOf(target)
  if (from < 0 || to < 0 || from === to) return
  moveEntry(from, to - from)
}

async function save(): Promise<void> {
  if (!props.token || !canWrite.value || saving.value) return
  if (loading.value || conflict.value) return
  if (validationError.value) {
    saveError.value = validationError.value
    return
  }
  if (!dirty.value) {
    emit('close')
    return
  }

  saving.value = true
  clearErrors()
  try {
    const selectedOnly = derivedSelectedOnly.value
    const saved = await tokenRoutingApi.save(props.token.id, {
      mode: draft.mode,
      selected_only: selectedOnly,
      version: version.value,
      max_channel_ratio: draft.maxRatio.trim() || null,
      entries: selectedOnly
        ? draft.entries.map((entry, position) => ({
            channel_id: entry.channel_id,
            enabled: entry.enabled,
            position,
          }))
        : [],
    })
    applyRoute(saved)
    toast.success(t('keys.routingPanel.saved'))
    emit('saved')
    emit('close')
  } catch (error) {
    const isConflict = error instanceof ApiError && error.status === 409
    conflict.value = isConflict
    saveError.value = isConflict
      ? t('keys.routingPanel.conflict')
      : error instanceof ApiError
        ? error.message
        : String(error)
  } finally {
    saving.value = false
  }
}

watch(
  [() => props.open, () => props.token?.id, () => props.initialMode, canRead],
  ([open]) => {
    if (!open || !props.token) {
      routeRequest.cancel()
      candidatesRequest.cancel()
      return
    }
    void loadAll()
  },
  { immediate: true }
)

watch(searchQuery, () => {
  visibleLimit.value = 20
})

onBeforeUnmount(() => {
  routeRequest.cancel()
  candidatesRequest.cancel()
})
</script>

<template>
  <ConsoleModal
    :open="open && token != null"
    :title="t('keys.routingPanel.modalTitle', { name: token?.name ?? '' })"
    size="lg"
    @close="emit('close')"
  >
    <div
      v-if="token"
      class="sibling-space-y-5 text-left"
      data-key-routing-modal
    >
      <!-- Save error or 409 conflict -->
      <div
        v-if="saveError"
        role="alert"
        data-routing-save-error
        class="no-handdrawn flex flex-wrap items-center justify-between gap-3 rounded-xl border p-3 text-xs"
        :class="
          conflict
            ? 'border-[var(--status-warning)] bg-[var(--status-warning-soft)] text-[var(--status-warning-text)]'
            : 'border-[var(--status-danger)] bg-[var(--status-danger-soft)] text-[var(--status-danger-text)]'
        "
      >
        <span class="min-w-0 flex-1 leading-relaxed">{{ saveError }}</span>
        <ConsoleButton
          v-if="conflict"
          variant="secondary"
          size="sm"
          @click="loadAll"
        >
          <RefreshCw :size="14" aria-hidden="true" />
          {{ t('keys.routingPanel.reload') }}
        </ConsoleButton>
      </div>

      <!-- Loading / load error -->
      <p
        v-if="loading"
        role="status"
        class="py-8 text-center text-xs text-[var(--text-tertiary-overlay,var(--text-tertiary))]"
      >
        {{ t('keys.routingPanel.loading') }}
      </p>
      <div
        v-else-if="loadError"
        role="alert"
        class="no-handdrawn flex flex-wrap items-center justify-between gap-3 rounded-xl border border-[var(--status-danger)] bg-[var(--status-danger-soft)] p-3 text-xs text-[var(--status-danger-text)]"
      >
        <span class="min-w-0 flex-1">{{
          t('keys.routingPanel.loadFailed', { error: loadError })
        }}</span>
        <ConsoleButton variant="secondary" size="sm" @click="loadAll">
          <RefreshCw :size="14" aria-hidden="true" />
          {{ t('keys.routingPanel.reload') }}
        </ConsoleButton>
      </div>

      <template v-else>
        <!-- Compact read-only policy summary (mode + multiplier cap live in KeyFormModal) -->
        <div
          data-routing-policy-summary
          class="no-handdrawn flex flex-wrap items-center justify-between gap-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-3.5 py-2.5 text-xs"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span class="font-medium text-[var(--text-primary)]">
              {{
                t('keys.routingPanel.policySummaryMode', {
                  mode: t(`keys.routingPanel.modes.${draft.mode}`),
                })
              }}
            </span>
            <span
              v-if="ordered && isAllPaused"
              class="rounded-md bg-[var(--surface-solid)] px-1.5 py-0.5 text-[0.6875rem] text-[var(--text-tertiary)]"
            >
              {{ t('keys.routingPanel.reasons.entry_disabled') }}
            </span>
            <span
              v-if="draft.maxRatio"
              class="tabular-nums text-[var(--text-secondary)]"
            >
              ·
              {{
                t('keys.routingPanel.policySummaryRatio', {
                  ratio: draft.maxRatio,
                })
              }}
            </span>
          </div>
          <button
            v-if="canWrite"
            type="button"
            data-routing-edit-policy
            class="min-h-[44px] rounded-lg px-2.5 py-1 text-xs font-medium text-[var(--accent-text)] transition-colors hover:bg-[var(--surface-solid)] focus-ring sm:min-h-[32px]"
            @click="emit('edit-policy', token)"
          >
            {{ t('keys.routingPanel.editPolicy') }}
          </button>
        </div>

        <!-- Read-only notice -->
        <div
          v-if="!canWrite"
          data-routing-readonly
          class="no-handdrawn flex items-center gap-2.5 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-3.5 py-2.5 text-xs text-[var(--text-secondary)]"
        >
          <Lock :size="14" class="shrink-0" aria-hidden="true" />
          <span>{{ t('keys.routingPanel.readOnlyEdit') }}</span>
        </div>

        <!-- Orphaned empty-selection warning -->
        <div
          v-if="draft.orphan"
          role="alert"
          data-route-orphaned
          class="no-handdrawn flex flex-col gap-3 rounded-xl border border-[var(--status-warning)] bg-[var(--status-warning-soft)] p-3.5 text-xs text-[var(--status-warning-text)]"
        >
          <div class="flex items-start gap-2">
            <AlertTriangle
              :size="15"
              class="mt-0.5 shrink-0"
              aria-hidden="true"
            />
            <span class="leading-relaxed">{{
              t('keys.routingPanel.orphanedNotice')
            }}</span>
          </div>
          <div v-if="canWrite" class="flex flex-wrap gap-2">
            <ConsoleButton
              variant="secondary"
              size="sm"
              :disabled="!canInteract"
              @click="usePlatform"
            >
              {{ t('keys.routingPanel.usePlatform') }}
            </ConsoleButton>
          </div>
        </div>

        <!-- Ordered empty validation hint -->
        <div
          v-if="ordered && draft.entries.length === 0 && !draft.orphan"
          role="alert"
          data-routing-ordered-empty
          class="no-handdrawn flex flex-wrap items-center justify-between gap-2 rounded-xl border border-[var(--status-warning)] bg-[var(--status-warning-soft)] px-3.5 py-2.5 text-xs text-[var(--status-warning-text)]"
        >
          <span>{{
            pendingOrderedIntent
              ? t('keys.routingPanel.pendingOrderedHint')
              : t('keys.routingPanel.orderedRequired')
          }}</span>
          <button
            v-if="canInteract"
            type="button"
            class="underline underline-offset-2 focus-ring"
            @click="usePlatform"
          >
            {{ t('keys.routingPanel.switchToAuto') }}
          </button>
        </div>

        <!-- All-paused fallback info notice -->
        <div
          v-if="isAllPaused"
          role="status"
          data-routing-all-paused
          class="no-handdrawn flex items-start gap-2 rounded-xl border border-[var(--status-info)] bg-[var(--status-info-soft)] p-3 text-xs text-[var(--status-info-text)]"
        >
          <Info :size="15" class="mt-0.5 shrink-0" aria-hidden="true" />
          <div class="min-w-0 flex-1 space-y-1">
            <p class="leading-relaxed">
              {{ t('keys.routingPanel.allPausedNotice') }}
            </p>
            <p v-if="ordered" class="opacity-85">
              {{ t('keys.routingPanel.orderedInactiveHint') }}
            </p>
          </div>
        </div>

        <!-- Reverted to platform note -->
        <p
          v-if="revertedNote && draft.entries.length === 0 && !draft.orphan"
          role="status"
          class="text-xs text-[var(--text-tertiary-overlay,var(--text-tertiary))]"
        >
          {{ t('keys.routingPanel.revertedToPlatform') }}
        </p>

        <!-- Selected channels list (active scope first) -->
        <div class="space-y-2" data-routing-selected-list>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div>
              <h3 class="text-xs font-semibold text-[var(--text-secondary)]">
                {{ t('keys.routingPanel.selectedTitle') }} ({{
                  draft.entries.length
                }})
              </h3>
              <p
                v-if="draft.entries.length > 0"
                class="text-[0.6875rem] text-[var(--text-tertiary-overlay,var(--text-tertiary))]"
              >
                {{
                  ordered
                    ? t('keys.routingPanel.orderedListHint')
                    : t('keys.routingPanel.autoListHint')
                }}
              </p>
            </div>
            <button
              v-if="canInteract && draft.entries.length > 0"
              type="button"
              class="min-h-[32px] rounded-lg px-2 py-1 text-xs font-medium text-[var(--text-secondary)] transition-colors hover:bg-[var(--surface-muted)] hover:text-[var(--text-primary)] focus-ring"
              @click="usePlatform"
            >
              {{ t('keys.routingPanel.clearToPlatform') }}
            </button>
          </div>

          <p
            v-if="draft.entries.length === 0 && !draft.orphan"
            class="rounded-xl border border-dashed border-[var(--border-subtle)] px-4 py-5 text-center text-xs text-[var(--text-tertiary-overlay,var(--text-tertiary))]"
          >
            {{
              ordered
                ? t('keys.routingPanel.emptyOrdered')
                : t('keys.routingPanel.emptyAuto')
            }}
          </p>

          <component
            :is="ordered ? 'ol' : 'ul'"
            v-else-if="draft.entries.length > 0"
            class="space-y-2"
            :aria-label="t('keys.routingPanel.selectedTitle')"
          >
            <li
              v-for="(entry, index) in draft.entries"
              :key="entry.channel_id"
              :draggable="canInteract && ordered"
              :data-selected-channel-id="entry.channel_id"
              class="no-handdrawn flex flex-col gap-2 rounded-xl border bg-[var(--surface-solid)] px-3 py-2.5 sm:flex-row sm:items-center"
              :class="[
                !channelFor(entry)
                  ? 'border-[var(--status-danger)] bg-[var(--status-danger-soft)]/40'
                  : !entry.enabled
                    ? 'border-[var(--border-subtle)] opacity-75'
                    : 'border-[var(--border-subtle)]',
                draggingChannelId === entry.channel_id ? 'opacity-50' : '',
              ]"
              @dragstart="startDrag($event, entry)"
              @dragend="draggingChannelId = null"
              @dragover.prevent
              @drop.prevent="dropEntry(entry)"
            >
              <div
                class="flex min-w-0 flex-1 items-start gap-2 sm:items-center"
              >
                <template v-if="ordered">
                  <GripVertical
                    v-if="canInteract"
                    :size="14"
                    class="hidden shrink-0 cursor-grab text-[var(--text-tertiary)] sm:inline-block"
                    aria-hidden="true"
                  />
                  <span
                    class="mt-0.5 inline-flex h-5 min-w-[1.75rem] shrink-0 items-center justify-center rounded-md bg-[var(--surface-muted)] px-1 font-mono text-[0.6875rem] font-semibold tabular-nums text-[var(--text-secondary)] sm:mt-0"
                  >
                    #{{ index + 1 }}
                  </span>
                </template>
                <div class="min-w-0 flex-1">
                  <p
                    class="break-words text-sm font-medium text-[var(--text-primary)]"
                  >
                    <template v-if="channelFor(entry)">
                      #{{ entry.channel_id }} {{ channelFor(entry)!.name }}
                    </template>
                    <span
                      v-else
                      class="text-[var(--status-danger-text)]"
                      data-unavailable-channel
                    >
                      {{ missingChannelText(entry.channel_id) }}
                    </span>
                  </p>
                  <div
                    class="mt-1 flex flex-wrap items-center gap-1.5 text-[0.6875rem] text-[var(--text-tertiary)]"
                  >
                    <StatusChip
                      v-if="channelFor(entry)"
                      class="routing-aux-chip"
                      :data-aux-eligible="
                        channelFor(entry)!.eligible ? '' : undefined
                      "
                      :tone="
                        channelFor(entry)!.eligible ? 'success' : 'warning'
                      "
                    >
                      {{
                        channelFor(entry)!.eligible
                          ? t('keys.routingPanel.reasons.eligible')
                          : reasonLabel(channelFor(entry)!.filter_reason)
                      }}
                    </StatusChip>
                    <span v-if="channelFor(entry)" class="tabular-nums">
                      {{
                        t('keys.routingPanel.selectedMetrics', {
                          ratio: channelFor(entry)!.channel_ratio,
                          limit: channelFor(entry)!.max_concurrency || '∞',
                        })
                      }}
                    </span>
                  </div>
                </div>
              </div>

              <div
                v-if="canWrite"
                class="flex flex-wrap items-center justify-end gap-1 border-t border-[var(--border-subtle)] pt-2 sm:border-t-0 sm:pt-0"
              >
                <label
                  class="mr-1 flex min-h-[44px] items-center gap-1.5 text-xs text-[var(--text-secondary)] sm:min-h-0"
                >
                  <ConsoleToggle
                    v-model="entry.enabled"
                    :label="`${t('keys.routingPanel.enabled')} #${entry.channel_id}`"
                    size="sm"
                    :disabled="!canInteract"
                    @update:model-value="clearErrors"
                  />
                  {{ t('keys.routingPanel.enabled') }}
                </label>
                <template v-if="ordered">
                  <IconButton
                    :label="t('keys.routingPanel.up')"
                    :disabled="!canInteract || index === 0"
                    @click="moveEntry(index, -1)"
                  >
                    <ArrowUp :size="15" aria-hidden="true" />
                  </IconButton>
                  <IconButton
                    :label="t('keys.routingPanel.down')"
                    :disabled="
                      !canInteract || index === draft.entries.length - 1
                    "
                    @click="moveEntry(index, 1)"
                  >
                    <ArrowDown :size="15" aria-hidden="true" />
                  </IconButton>
                </template>
                <IconButton
                  :label="t('keys.routingPanel.remove')"
                  tone="danger"
                  :disabled="!canInteract"
                  @click="removeEntry(index)"
                >
                  <Trash2 :size="15" aria-hidden="true" />
                </IconButton>
              </div>
            </li>
          </component>
        </div>

        <!-- Candidate channel picker (always open when writable; 1 col <640px, 2 cols >=640px) -->
        <div
          v-if="canWrite"
          data-routing-candidate-picker
          class="space-y-3 border-t border-[var(--border-subtle)] pt-4"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h3 class="text-xs font-semibold text-[var(--text-secondary)]">
              {{ t('keys.routingPanel.candidateTitle') }}
            </h3>
            <div
              v-if="modelLimits.length > 0 || draft.maxRatio.trim()"
              class="flex flex-wrap gap-1.5 text-[0.6875rem] text-[var(--text-tertiary-overlay,var(--text-tertiary))]"
            >
              <span v-if="modelLimits.length > 0">{{
                t('keys.routingPanel.filteredByModels')
              }}</span>
              <span v-if="draft.maxRatio.trim()">{{
                t('keys.routingPanel.filteredByRatio', {
                  ratio: draft.maxRatio.trim(),
                })
              }}</span>
            </div>
          </div>

          <!-- Search & filter bar -->
          <div class="flex flex-col gap-2 sm:flex-row sm:items-center">
            <div class="relative min-w-0 flex-1">
              <Search
                :size="14"
                class="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-[var(--text-tertiary)]"
                aria-hidden="true"
              />
              <input
                v-model="searchQuery"
                type="search"
                name="routing-channel-search"
                :aria-label="t('keys.routingPanel.searchPlaceholder')"
                :placeholder="t('keys.routingPanel.searchPlaceholder')"
                class="h-11 w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] pr-3 pl-8 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] focus:border-[var(--border-strong)] focus-ring sm:h-9"
              />
            </div>
            <div class="flex flex-wrap items-center gap-1.5">
              <button
                type="button"
                :aria-pressed="onlyEligible"
                class="no-handdrawn min-h-[36px] rounded-lg border px-2.5 py-1 text-xs font-medium transition-colors focus-ring"
                :class="
                  onlyEligible
                    ? 'border-[var(--accent)] bg-[var(--accent-soft)] text-[var(--accent-text)]'
                    : 'border-[var(--border-subtle)] bg-[var(--surface-muted)] text-[var(--text-secondary)] hover:border-[var(--border-strong)]'
                "
                @click="onlyEligible = !onlyEligible"
              >
                {{ t('keys.routingPanel.filterEligible') }}
              </button>
              <button
                type="button"
                :aria-pressed="sortByRatio"
                class="no-handdrawn min-h-[36px] rounded-lg border px-2.5 py-1 text-xs font-medium transition-colors focus-ring"
                :class="
                  sortByRatio
                    ? 'border-[var(--accent)] bg-[var(--accent-soft)] text-[var(--accent-text)]'
                    : 'border-[var(--border-subtle)] bg-[var(--surface-muted)] text-[var(--text-secondary)] hover:border-[var(--border-strong)]'
                "
                @click="sortByRatio = !sortByRatio"
              >
                {{ t('keys.routingPanel.sortByRatio') }}
              </button>
            </div>
          </div>

          <p
            v-if="candidatesLoading"
            role="status"
            class="py-4 text-center text-xs text-[var(--text-tertiary-overlay,var(--text-tertiary))]"
          >
            {{ t('keys.routingPanel.loadingCandidates') }}
          </p>
          <div
            v-else-if="candidatesError"
            role="alert"
            class="no-handdrawn flex flex-wrap items-center justify-between gap-2 rounded-lg border border-[var(--status-danger)] bg-[var(--status-danger-soft)] p-2.5 text-xs text-[var(--status-danger-text)]"
          >
            <span>{{
              t('keys.routingPanel.candidatesFailed', {
                error: candidatesError,
              })
            }}</span>
            <ConsoleButton
              variant="secondary"
              size="sm"
              @click="fetchCandidates"
            >
              {{ t('keys.routingPanel.reload') }}
            </ConsoleButton>
          </div>
          <p
            v-else-if="channels.length === 0"
            class="py-4 text-center text-xs text-[var(--text-tertiary-overlay,var(--text-tertiary))]"
          >
            {{ t('keys.routingPanel.noChannels') }}
          </p>
          <p
            v-else-if="visibleChannels.length === 0"
            class="py-4 text-center text-xs text-[var(--text-tertiary-overlay,var(--text-tertiary))]"
          >
            {{ t('keys.routingPanel.noMatches') }}
          </p>
          <div v-else class="space-y-2">
            <div
              class="grid grid-cols-1 gap-2 sm:grid-cols-2"
              role="group"
              :aria-label="t('keys.routingPanel.candidateTitle')"
            >
              <button
                v-for="channel in visibleChannels"
                :key="channel.id"
                type="button"
                role="checkbox"
                :aria-checked="selectedIds.has(channel.id)"
                :disabled="
                  !canInteract ||
                  (!channel.eligible && !selectedIds.has(channel.id))
                "
                :data-candidate-channel-id="channel.id"
                class="no-handdrawn flex min-h-[44px] w-full items-start gap-2.5 rounded-xl border px-3 py-2.5 text-left transition-colors focus-ring disabled:cursor-not-allowed disabled:opacity-55"
                :class="
                  selectedIds.has(channel.id)
                    ? 'border-[var(--accent)] bg-[var(--accent-soft)]'
                    : 'border-[var(--border-subtle)] bg-[var(--surface-solid)] hover:border-[var(--border-strong)]'
                "
                @click="toggleCandidate(channel)"
              >
                <span
                  class="mt-0.5 grid h-4 w-4 shrink-0 place-items-center rounded-[4px] border transition-colors"
                  :class="
                    selectedIds.has(channel.id)
                      ? 'border-[var(--accent)] bg-[var(--accent)] text-[var(--accent-contrast)]'
                      : 'border-[var(--border-strong)] bg-[var(--surface-solid)]'
                  "
                  aria-hidden="true"
                >
                  <Check
                    v-if="selectedIds.has(channel.id)"
                    :size="11"
                    :stroke-width="3"
                  />
                </span>
                <span class="min-w-0 flex-1">
                  <span
                    class="block break-words text-sm font-medium"
                    :class="
                      selectedIds.has(channel.id)
                        ? 'text-[var(--accent-text)]'
                        : 'text-[var(--text-primary)]'
                    "
                  >
                    #{{ channel.id }} {{ channel.name }}
                  </span>
                  <span
                    class="mt-0.5 flex flex-wrap items-center gap-1.5 text-[0.6875rem] tabular-nums text-[var(--text-tertiary)]"
                  >
                    <span>{{
                      t('keys.routingPanel.channelMetrics', {
                        ratio: channel.channel_ratio,
                        models: channel.models.length,
                        limit: channel.max_concurrency || '∞',
                      })
                    }}</span>
                    <span
                      v-if="!channel.eligible"
                      class="rounded bg-[var(--status-warning-soft)] px-1.5 py-0.5 text-[var(--status-warning-text)]"
                    >
                      {{ reasonLabel(channel.filter_reason) }}
                    </span>
                  </span>
                </span>
              </button>
            </div>

            <ConsoleButton
              v-if="remainingCount > 0"
              variant="secondary"
              size="sm"
              block
              @click="visibleLimit += 20"
            >
              {{ t('keys.routingPanel.showMore', { count: remainingCount }) }}
            </ConsoleButton>
          </div>
        </div>
      </template>
    </div>

    <template #footer>
      <div v-if="canWrite" class="grid grid-cols-2 gap-3">
        <ConsoleButton
          variant="secondary"
          size="lg"
          :disabled="saving"
          @click="emit('close')"
        >
          {{ t('common.cancel') }}
        </ConsoleButton>
        <ConsoleButton
          size="lg"
          :loading="saving"
          :disabled="!canSave"
          @click="save"
        >
          {{ t('common.save') }}
        </ConsoleButton>
      </div>
      <ConsoleButton
        v-else
        size="lg"
        block
        variant="secondary"
        @click="emit('close')"
      >
        {{ t('common.close') }}
      </ConsoleButton>
    </template>
  </ConsoleModal>
</template>

<style scoped>
:global(html.dark .routing-aux-chip[data-aux-eligible]) {
  background: var(--surface-muted) !important;
  color: var(--text-secondary) !important;
  border-left-color: var(--border-default) !important;
}
</style>
