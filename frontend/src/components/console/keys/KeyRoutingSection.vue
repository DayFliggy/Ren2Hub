<script setup lang="ts">
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Check,
  ChevronDown,
  GripVertical,
  Info,
  Plus,
  RefreshCw,
  Route,
  Search,
  Trash2,
} from 'lucide-vue-next'
import { computed, onBeforeUnmount, reactive, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  tokenRoutingApi,
  type TokenRoute,
  type TokenRouteChannel,
  type TokenRouteMode,
  type TokenRouteScope,
} from '@/api/tokenRouting'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import FormField from '@/components/common/FormField.vue'
import IconButton from '@/components/common/IconButton.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { isValidChannelRatio } from '@/utils/channelRatio'

export interface RoutingSaveState {
  loading: boolean
  loadError: string
  validationError: string
  dirty: boolean
  isDefault: boolean
  version: number
  payload: Pick<
    TokenRoute,
    'mode' | 'selected_only' | 'version' | 'entries' | 'max_channel_ratio'
  >
}

const props = defineProps<{
  open: boolean
  editingId: number | null
  createdTokenId?: number | null
  modelLimits: string[]
  canRead: boolean
  canWrite: boolean
  autoExpand: boolean
  disabled: boolean
  saveError: string
  conflict: boolean
  partialNotice: string
}>()

const emit = defineEmits<{
  'clear-error': []
  'retry-routing': []
}>()

const { t, te } = useI18n()
const sectionId = useId()
const sectionEl = ref<HTMLElement | null>(null)

const expanded = ref(false)
const pickerOpen = ref(false)
const ratioOpen = ref(false)
const revertedNote = ref(false)

const loading = ref(false)
const loadError = ref('')
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
let candidateDebounceTimer = 0

interface DraftEntry {
  channel_id: number
  enabled: boolean
}

interface NormalizedSnapshot {
  mode: TokenRouteMode
  selected_only: boolean
  max_channel_ratio: string | null
  entries: Array<[number, boolean]>
}

const draft = reactive({
  mode: 'auto' as TokenRouteMode,
  entries: [] as DraftEntry[],
  maxRatio: '',
  orphan: false,
})

const version = ref(0)
let baselineRatioRaw = ''
let baseline: NormalizedSnapshot = defaultSnapshot()

function defaultSnapshot(): NormalizedSnapshot {
  return {
    mode: 'auto',
    selected_only: false,
    max_channel_ratio: null,
    entries: [],
  }
}

/** Canonicalizes valid decimal ratio strings so "1" and "1.0" compare equal. */
function canonicalRatio(raw: string): string | null {
  const trimmed = raw.trim()
  if (!trimmed) return null
  if (!isValidChannelRatio(trimmed)) return trimmed
  const [intRaw = '0', fracRaw = ''] = trimmed.split('.')
  const intPart = intRaw.replace(/^0+(?=\d)/, '') || '0'
  const fracPart = fracRaw.replace(/0+$/, '')
  return fracPart ? `${intPart}.${fracPart}` : intPart
}

function buildSnapshot(
  mode: TokenRouteMode,
  selectedOnly: boolean,
  ratio: string,
  entries: DraftEntry[]
): NormalizedSnapshot {
  const effectiveEntries = selectedOnly ? entries : []
  return {
    mode,
    selected_only: selectedOnly,
    max_channel_ratio: canonicalRatio(ratio),
    entries: effectiveEntries.map((entry) => [entry.channel_id, entry.enabled]),
  }
}

function snapshotsEqual(a: NormalizedSnapshot, b: NormalizedSnapshot): boolean {
  if (
    a.mode !== b.mode ||
    a.selected_only !== b.selected_only ||
    a.max_channel_ratio !== b.max_channel_ratio ||
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
const validRatio = computed(() => {
  const value = draft.maxRatio.trim()
  return !value || isValidChannelRatio(value)
})

const effectiveScope = computed<TokenRouteScope>(() => {
  if (!derivedSelectedOnly.value) return 'platform'
  if (draft.entries.length === 0) return 'empty'
  if (enabledCount.value === 0) return 'platform'
  return 'selected'
})

const currentSnapshot = computed(() =>
  buildSnapshot(
    draft.mode,
    derivedSelectedOnly.value,
    draft.maxRatio === baselineRatioRaw ? baselineRatioRaw : draft.maxRatio,
    draft.entries
  )
)

const dirty = computed(() => !snapshotsEqual(currentSnapshot.value, baseline))
const isDefault = computed(() =>
  snapshotsEqual(currentSnapshot.value, defaultSnapshot())
)

const canInteract = computed(
  () =>
    props.canRead &&
    props.canWrite &&
    !props.disabled &&
    !loading.value &&
    !loadError.value &&
    !props.conflict
)

const validationError = computed(() => {
  if (!props.canWrite) return ''
  // In edit mode, untouched routing is not submitted so an existing orphan
  // state must not block unrelated edits (e.g. renaming or quota changes).
  if (props.editingId !== null && !dirty.value) return ''
  if (!validRatio.value) return t('keys.routingPanel.invalidRatio')
  if (draft.orphan) return t('keys.routingPanel.orphanedRequired')
  if (draft.mode === 'ordered' && draft.entries.length === 0) {
    return t('keys.routingPanel.orderedRequired')
  }
  return ''
})

const channelById = computed(
  () => new Map(channels.value.map((channel) => [channel.id, channel]))
)
const selectedIds = computed(
  () => new Set(draft.entries.map((entry) => entry.channel_id))
)
const eligiblePlatformCount = computed(
  () => channels.value.filter((channel) => channel.eligible).length
)

const summaryText = computed(() => {
  if (draft.orphan) return t('keys.routingPanel.summaryOrphaned')
  if (isAllPaused.value) return t('keys.routingPanel.summaryAllPaused')
  if (effectiveScope.value === 'selected') {
    return ordered.value
      ? t(
          'keys.routingPanel.summarySelectedOrdered',
          { count: enabledCount.value },
          enabledCount.value
        )
      : t(
          'keys.routingPanel.summarySelectedAuto',
          { count: enabledCount.value },
          enabledCount.value
        )
  }
  if (
    !candidatesLoading.value &&
    !candidatesError.value &&
    channels.value.length > 0
  ) {
    return t('keys.routingPanel.summaryPlatformCount', {
      count: eligiblePlatformCount.value,
    })
  }
  return t('keys.routingPanel.summaryPlatform')
})

const filteredChannels = computed(() => {
  const kw = searchQuery.value.trim().toLowerCase()
  let list = channels.value.filter((channel) => {
    if (onlyEligible.value && !channel.eligible) return false
    if (!kw) return true
    if (channel.name.toLowerCase().includes(kw)) return true
    if (`#${channel.id}`.includes(kw) || String(channel.id).includes(kw))
      return true
    return channel.models.some((model) => model.toLowerCase().includes(kw))
  })
  if (sortByRatio.value) {
    list = [...list].sort(
      (a, b) => Number(a.channel_ratio) - Number(b.channel_ratio) || a.id - b.id
    )
  }
  return list
})

const visibleChannels = computed(() =>
  filteredChannels.value.slice(0, visibleLimit.value)
)
const remainingCount = computed(() =>
  Math.max(0, filteredChannels.value.length - visibleChannels.value.length)
)

function reasonLabel(reason?: string): string {
  if (!reason) return t('keys.routingPanel.reasons.channel_unavailable')
  return te(`keys.routingPanel.reasons.${reason}`)
    ? t(`keys.routingPanel.reasons.${reason}`)
    : reason
}

function channelFor(entry: DraftEntry): TokenRouteChannel | undefined {
  return channelById.value.get(entry.channel_id)
}

function applyRoute(route: TokenRoute): void {
  version.value = route.version
  draft.mode = route.mode
  draft.entries = route.entries.map((entry) => ({
    channel_id: entry.channel_id,
    enabled: entry.enabled,
  }))
  draft.maxRatio = route.max_channel_ratio ?? ''
  baselineRatioRaw = draft.maxRatio
  draft.orphan = route.selected_only && route.entries.length === 0
  baseline = buildSnapshot(
    route.mode,
    route.selected_only,
    draft.maxRatio,
    draft.entries
  )
  revertedNote.value = false
  if (draft.maxRatio) ratioOpen.value = true
  if (
    props.autoExpand ||
    draft.orphan ||
    draft.entries.length > 0 ||
    draft.mode === 'ordered' ||
    Boolean(draft.maxRatio)
  ) {
    expanded.value = true
  }
}

function resetForCreate(): void {
  version.value = 0
  draft.mode = 'auto'
  draft.entries = []
  draft.maxRatio = ''
  baselineRatioRaw = ''
  draft.orphan = false
  baseline = defaultSnapshot()
  revertedNote.value = false
  ratioOpen.value = false
  pickerOpen.value = false
  expanded.value = props.autoExpand
  loadError.value = ''
  loading.value = false
}

let lastCandidateFilterKey = ''

function candidateFilterKey(limits: readonly string[], ratio: string): string {
  return `${limits.join('\u0000')}|${ratio}`
}

async function fetchCandidates(): Promise<void> {
  if (!props.open || !props.canRead) return
  candidatesLoading.value = true
  candidatesError.value = ''
  const ratio = validRatio.value ? draft.maxRatio.trim() : ''
  const limits = [...props.modelLimits]
  lastCandidateFilterKey = candidateFilterKey(limits, ratio)
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
    lastCandidateFilterKey = ''
    candidatesError.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
  }
}

function scheduleCandidatesFetch(): void {
  const ratio = validRatio.value ? draft.maxRatio.trim() : ''
  if (candidateFilterKey(props.modelLimits, ratio) === lastCandidateFilterKey) {
    return
  }
  window.clearTimeout(candidateDebounceTimer)
  candidateDebounceTimer = window.setTimeout(() => {
    void fetchCandidates()
  }, 250)
}

async function loadAll(): Promise<void> {
  window.clearTimeout(candidateDebounceTimer)
  routeRequest.cancel()
  candidatesRequest.cancel()
  lastCandidateFilterKey = ''
  searchQuery.value = ''
  onlyEligible.value = false
  sortByRatio.value = false
  visibleLimit.value = 20
  draggingChannelId.value = null
  channels.value = []
  candidatesError.value = ''
  loadError.value = ''

  if (!props.open || !props.canRead) {
    loading.value = false
    candidatesLoading.value = false
    return
  }

  const id = props.editingId ?? props.createdTokenId ?? null
  if (id === null) {
    resetForCreate()
    await fetchCandidates()
    return
  }

  loading.value = true
  candidatesLoading.value = true
  const limits = [...props.modelLimits]

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
  emit('clear-error')

  const ratio = routeResult.value.max_channel_ratio ?? ''
  lastCandidateFilterKey = candidateFilterKey(limits, ratio)
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
    lastCandidateFilterKey = ''
    candidatesError.value =
      candResult.error instanceof Error
        ? candResult.error.message
        : String(candResult.error)
  }
}

function setMode(mode: TokenRouteMode): void {
  if (!canInteract.value) return
  emit('clear-error')
  draft.mode = mode
  revertedNote.value = false
  if (mode === 'ordered' && draft.entries.length === 0) {
    pickerOpen.value = true
  }
}

function usePlatform(): void {
  if (!canInteract.value) return
  emit('clear-error')
  draft.mode = 'auto'
  draft.entries = []
  draft.orphan = false
  revertedNote.value = false
}

function toggleCandidate(channel: TokenRouteChannel): void {
  if (!canInteract.value) return
  emit('clear-error')
  const idx = draft.entries.findIndex(
    (entry) => entry.channel_id === channel.id
  )
  if (idx >= 0) {
    removeEntry(idx)
    return
  }
  if (!channel.eligible) return
  draft.orphan = false
  revertedNote.value = false
  draft.entries.push({ channel_id: channel.id, enabled: true })
}

function removeEntry(index: number): void {
  if (!canInteract.value) return
  emit('clear-error')
  draft.entries.splice(index, 1)
  if (draft.entries.length === 0 && draft.mode === 'auto') {
    draft.orphan = false
    revertedNote.value = true
  }
}

function moveEntry(index: number, offset: number): void {
  const target = index + offset
  if (
    !canInteract.value ||
    !ordered.value ||
    target < 0 ||
    target >= draft.entries.length
  ) {
    return
  }
  emit('clear-error')
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

function markSaved(saved: TokenRoute): void {
  applyRoute(saved)
}

function getState(): RoutingSaveState {
  const selectedOnly = derivedSelectedOnly.value
  return {
    loading: loading.value,
    loadError: loadError.value,
    validationError: validationError.value,
    dirty: dirty.value,
    isDefault: isDefault.value,
    version: version.value,
    payload: {
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
    },
  }
}

function scrollIntoView(): void {
  expanded.value = true
  sectionEl.value?.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' })
}

defineExpose({
  getState,
  reload: loadAll,
  markSaved,
  scrollIntoView,
})

watch(
  [() => props.open, () => props.editingId, () => props.canRead],
  ([open]) => {
    if (!open) {
      window.clearTimeout(candidateDebounceTimer)
      routeRequest.cancel()
      candidatesRequest.cancel()
      return
    }
    void loadAll()
  },
  { immediate: true }
)

watch(
  () => props.autoExpand,
  (val) => {
    if (val && props.open) expanded.value = true
  }
)

watch(
  () => [props.saveError, props.partialNotice, props.conflict],
  ([err, partial, conflict]) => {
    if (err || partial || conflict) expanded.value = true
  }
)

watch(
  () => props.modelLimits.join(','),
  () => {
    if (!props.open || !props.canRead || loading.value) return
    scheduleCandidatesFetch()
  }
)

watch(
  () => draft.maxRatio,
  () => {
    emit('clear-error')
    if (!props.open || !props.canRead || loading.value || !validRatio.value)
      return
    scheduleCandidatesFetch()
  }
)

watch(searchQuery, () => {
  visibleLimit.value = 20
})

onBeforeUnmount(() => {
  window.clearTimeout(candidateDebounceTimer)
  routeRequest.cancel()
  candidatesRequest.cancel()
})
</script>

<template>
  <section
    v-if="canRead"
    ref="sectionEl"
    data-key-routing-section
    class="overflow-hidden rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
  >
    <button
      type="button"
      data-key-routing-toggle
      class="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-[var(--surface-muted)] focus-ring"
      :aria-expanded="expanded"
      :aria-controls="sectionId"
      @click="expanded = !expanded"
    >
      <span
        class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg"
        :class="
          draft.orphan
            ? 'bg-[var(--status-warning-soft)] text-[var(--status-warning-text)]'
            : 'bg-[var(--accent-soft)] text-[var(--accent-text)]'
        "
        aria-hidden="true"
      >
        <Route :size="16" />
      </span>
      <span class="min-w-0 flex-1">
        <span class="block text-sm font-semibold text-[var(--text-primary)]">
          {{ t('keys.routingPanel.title') }}
        </span>
        <span class="block truncate text-xs text-[var(--text-tertiary)]">
          {{ loading ? t('keys.routingPanel.loading') : summaryText }}
        </span>
      </span>
      <span
        v-if="!expanded && canWrite"
        class="shrink-0 rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-2.5 py-1 text-xs font-medium text-[var(--text-secondary)]"
      >
        {{ t('keys.routingPanel.configure') }}
      </span>
      <ChevronDown
        :size="16"
        class="shrink-0 text-[var(--text-tertiary)] transition-transform"
        :class="{ 'rotate-180': expanded }"
        aria-hidden="true"
      />
    </button>

    <div
      v-if="expanded"
      :id="sectionId"
      class="sibling-space-y-4 border-t border-[var(--border-subtle)] bg-[var(--surface-muted)]/50 px-4 py-4"
    >
      <!-- Partial create/update failure alert -->
      <div
        v-if="partialNotice"
        role="alert"
        data-routing-partial-error
        class="no-handdrawn flex flex-wrap items-center justify-between gap-3 rounded-xl border border-[var(--status-warning)] bg-[var(--status-warning-soft)] p-3 text-xs text-[var(--status-warning-text)]"
      >
        <span class="min-w-0 flex-1 leading-relaxed">{{ partialNotice }}</span>
        <ConsoleButton
          variant="secondary"
          size="sm"
          :disabled="disabled"
          @click="emit('retry-routing')"
        >
          {{ t('keys.routingPanel.retryRouting') }}
        </ConsoleButton>
      </div>

      <!-- Save error or 409 conflict -->
      <div
        v-else-if="saveError"
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
        class="py-4 text-center text-xs text-[var(--text-tertiary)]"
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
        <!-- Read-only notice -->
        <p
          v-if="!canWrite"
          class="no-handdrawn rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3.5 py-2.5 text-xs text-[var(--text-secondary)]"
        >
          {{
            editingId === null
              ? t('keys.routingPanel.readOnlyCreate')
              : t('keys.routingPanel.readOnlyEdit')
          }}
        </p>

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
              @click="pickerOpen = true"
            >
              <Plus :size="14" aria-hidden="true" />
              {{ t('keys.routingPanel.pickChannels') }}
            </ConsoleButton>
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

        <!-- Mode segmented control (hidden on read-only create) -->
        <fieldset
          v-if="canWrite || editingId !== null"
          :disabled="!canInteract"
          class="min-w-0"
        >
          <legend class="mb-2 text-xs font-medium text-[var(--text-secondary)]">
            {{ t('keys.routingPanel.modeLabel') }}
          </legend>
          <div
            class="grid grid-cols-1 gap-2 sm:grid-cols-2"
            role="radiogroup"
            :aria-label="t('keys.routingPanel.modeLabel')"
          >
            <button
              v-for="mode in ['auto', 'ordered'] as const"
              :key="mode"
              type="button"
              role="radio"
              :aria-checked="draft.mode === mode"
              :disabled="!canInteract"
              class="no-handdrawn flex min-h-[44px] flex-col rounded-xl border px-3.5 py-2.5 text-left transition-colors focus-ring disabled:cursor-not-allowed disabled:opacity-60"
              :class="
                draft.mode === mode
                  ? 'border-[var(--accent)] bg-[var(--accent-soft)]'
                  : 'border-[var(--border-subtle)] bg-[var(--surface-solid)] hover:border-[var(--border-strong)]'
              "
              @click="setMode(mode)"
            >
              <span class="flex w-full items-center justify-between gap-2">
                <span
                  class="text-sm font-semibold"
                  :class="
                    draft.mode === mode
                      ? 'text-[var(--accent-text)]'
                      : 'text-[var(--text-primary)]'
                  "
                >
                  {{ t(`keys.routingPanel.modes.${mode}`) }}
                </span>
                <span
                  v-if="mode === 'ordered' && isAllPaused"
                  class="rounded-md bg-[var(--surface-muted)] px-1.5 py-0.5 text-[0.6875rem] text-[var(--text-tertiary)]"
                >
                  {{ t('keys.routingPanel.reasons.entry_disabled') }}
                </span>
              </span>
              <span
                class="mt-1 text-xs leading-relaxed text-[var(--text-secondary)]"
              >
                {{ t(`keys.routingPanel.modeHints.${mode}`) }}
              </span>
            </button>
          </div>
        </fieldset>

        <!-- Ordered empty validation hint -->
        <div
          v-if="ordered && draft.entries.length === 0 && !draft.orphan"
          role="alert"
          data-routing-ordered-empty
          class="no-handdrawn flex flex-wrap items-center justify-between gap-2 rounded-xl border border-[var(--status-warning)] bg-[var(--status-warning-soft)] px-3.5 py-2.5 text-xs text-[var(--status-warning-text)]"
        >
          <span>{{ t('keys.routingPanel.orderedRequired') }}</span>
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
          class="text-xs text-[var(--text-tertiary)]"
        >
          {{ t('keys.routingPanel.revertedToPlatform') }}
        </p>

        <!-- Selected channels list -->
        <div
          v-if="draft.entries.length > 0"
          class="space-y-2"
          data-routing-selected-list
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div>
              <h3 class="text-xs font-semibold text-[var(--text-secondary)]">
                {{ t('keys.routingPanel.selectedTitle') }} ({{
                  draft.entries.length
                }})
              </h3>
              <p class="text-[0.6875rem] text-[var(--text-tertiary)]">
                {{
                  ordered
                    ? t('keys.routingPanel.orderedListHint')
                    : t('keys.routingPanel.autoListHint')
                }}
              </p>
            </div>
            <button
              v-if="canInteract"
              type="button"
              class="min-h-[32px] rounded-lg px-2 py-1 text-xs font-medium text-[var(--text-secondary)] transition-colors hover:bg-[var(--surface-solid)] hover:text-[var(--text-primary)] focus-ring"
              @click="usePlatform"
            >
              {{ t('keys.routingPanel.clearToPlatform') }}
            </button>
          </div>

          <component
            :is="ordered ? 'ol' : 'ul'"
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
                      {{
                        t('keys.routingPanel.unavailableChannel', {
                          id: entry.channel_id,
                        })
                      }}
                    </span>
                  </p>
                  <div
                    class="mt-1 flex flex-wrap items-center gap-1.5 text-[0.6875rem] text-[var(--text-tertiary)]"
                  >
                    <StatusChip
                      v-if="channelFor(entry)"
                      class="routing-aux-chip"
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
                    @update:model-value="emit('clear-error')"
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

        <!-- Inline candidate channel picker (no second modal, no nested scroll) -->
        <div v-if="canWrite" class="space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <ConsoleButton
              variant="secondary"
              size="sm"
              data-routing-picker-toggle
              :disabled="!canInteract"
              @click="pickerOpen = !pickerOpen"
            >
              <Plus v-if="!pickerOpen" :size="14" aria-hidden="true" />
              <ChevronDown
                v-else
                :size="14"
                class="rotate-180"
                aria-hidden="true"
              />
              {{
                pickerOpen
                  ? t('keys.routingPanel.hideCandidates')
                  : t('keys.routingPanel.pickChannels')
              }}
            </ConsoleButton>
            <div
              v-if="
                modelLimits.length > 0 || (validRatio && draft.maxRatio.trim())
              "
              class="flex flex-wrap gap-1.5 text-[0.6875rem] text-[var(--text-tertiary)]"
            >
              <span v-if="modelLimits.length > 0">{{
                t('keys.routingPanel.filteredByModels')
              }}</span>
              <span v-if="validRatio && draft.maxRatio.trim()">{{
                t('keys.routingPanel.filteredByRatio', {
                  ratio: draft.maxRatio.trim(),
                })
              }}</span>
            </div>
          </div>

          <div
            v-if="pickerOpen"
            data-routing-candidate-picker
            class="space-y-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3"
          >
            <!-- Search & filter bar -->
            <div class="flex flex-col gap-2 sm:flex-row sm:items-center">
              <div class="relative min-w-0 flex-1">
                <Search
                  :size="14"
                  class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-tertiary)]"
                  aria-hidden="true"
                />
                <input
                  v-model="searchQuery"
                  type="search"
                  name="routing-channel-search"
                  :aria-label="t('keys.routingPanel.searchPlaceholder')"
                  :placeholder="t('keys.routingPanel.searchPlaceholder')"
                  class="h-11 w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] pl-8 pr-3 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] focus:border-[var(--border-strong)] focus-ring sm:h-9"
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
              class="py-4 text-center text-xs text-[var(--text-tertiary)]"
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
              class="py-4 text-center text-xs text-[var(--text-tertiary)]"
            >
              {{ t('keys.routingPanel.noChannels') }}
            </p>
            <p
              v-else-if="visibleChannels.length === 0"
              class="py-4 text-center text-xs text-[var(--text-tertiary)]"
            >
              {{ t('keys.routingPanel.noMatches') }}
            </p>
            <div v-else class="space-y-1.5" role="group">
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
                class="no-handdrawn flex min-h-[44px] w-full items-center gap-3 rounded-lg border px-3 py-2 text-left transition-colors focus-ring disabled:cursor-not-allowed disabled:opacity-55"
                :class="
                  selectedIds.has(channel.id)
                    ? 'border-[var(--accent)] bg-[var(--accent-soft)]'
                    : 'border-[var(--border-subtle)] bg-[var(--surface-solid)] hover:border-[var(--border-strong)]'
                "
                @click="toggleCandidate(channel)"
              >
                <span
                  class="grid h-4 w-4 shrink-0 place-items-center rounded-[4px] border transition-colors"
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
        </div>

        <!-- Collapsible multiplier cap -->
        <div
          v-if="canWrite || draft.maxRatio"
          class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
        >
          <button
            type="button"
            class="flex min-h-[44px] w-full items-center justify-between gap-2 px-3.5 py-2 text-left text-xs font-medium text-[var(--text-secondary)] transition-colors hover:bg-[var(--surface-muted)] focus-ring sm:min-h-[38px]"
            :aria-expanded="ratioOpen"
            @click="ratioOpen = !ratioOpen"
          >
            <span>
              {{ t('keys.routingPanel.moreOptions') }}
              <span
                v-if="draft.maxRatio.trim()"
                class="ml-1.5 font-mono text-[var(--accent-text)]"
              >
                ≤ ×{{ draft.maxRatio.trim() }}
              </span>
            </span>
            <ChevronDown
              :size="14"
              class="shrink-0 text-[var(--text-tertiary)] transition-transform"
              :class="{ 'rotate-180': ratioOpen }"
              aria-hidden="true"
            />
          </button>
          <div
            v-if="ratioOpen"
            class="border-t border-[var(--border-subtle)] px-3.5 py-3"
          >
            <FormField
              :label="t('keys.routingPanel.maxRatio')"
              :hint="t('keys.routingPanel.maxRatioHint')"
            >
              <TextInput
                v-model="draft.maxRatio"
                name="token-max-channel-ratio"
                inputmode="decimal"
                maxlength="24"
                :aria-label="t('keys.routingPanel.maxRatio')"
                :aria-invalid="!validRatio"
                :placeholder="t('keys.routingPanel.maxRatioPlaceholder')"
                :disabled="!canInteract"
              />
            </FormField>
            <p
              v-if="!validRatio"
              role="alert"
              class="mt-1.5 text-xs text-[var(--status-danger-text)]"
            >
              {{ t('keys.routingPanel.invalidRatio') }}
            </p>
          </div>
        </div>
      </template>
    </div>
  </section>
</template>

<style scoped>
:global(html.dark .routing-aux-chip[data-tone='success']) {
  background: var(--surface-muted) !important;
  color: var(--text-secondary) !important;
  border-left-color: var(--border-default) !important;
}
</style>
