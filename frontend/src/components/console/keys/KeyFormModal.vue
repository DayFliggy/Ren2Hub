<script setup lang="ts">
import { AlertTriangle, Info, Lock, RefreshCw } from 'lucide-vue-next'
import { computed, onBeforeUnmount, reactive, ref, useId, watch } from 'vue'
import { getActivePinia } from 'pinia'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import {
  tokenRoutingApi,
  type TokenRoute,
  type TokenRouteEntry,
  type TokenRouteMode,
} from '@/api/tokenRouting'
import { ApiError } from '@/api/types'
import type { TokenSummary } from '@/types/console'
import ChipPicker from '@/components/common/ChipPicker.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import { tokenRoutingAccess } from '@/constants/navigation/consoleNav'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { isValidChannelRatio } from '@/utils/channelRatio'
import { formatTime, fromDateTimeLocal, toDateTimeLocal } from '@/utils/format'
import { decimalToQuota, quotaToDecimal } from '@/utils/quota'

const props = defineProps<{
  open: boolean
  /** null = create mode */
  editing: TokenSummary | null
  models: string[]
}>()

const emit = defineEmits<{
  close: []
  saved: []
  'open-routing': [
    payload: { token: TokenSummary; initialMode?: TokenRouteMode },
  ]
}>()

const { t } = useI18n()
const toast = useToast()

const routingFeatureOn = computed(() => {
  if (!getActivePinia()) return false
  return useAppStore().isFeatureEnabled(tokenRoutingAccess.feature)
})
const canReadRouting = computed(() => {
  if (!routingFeatureOn.value || !getActivePinia()) return false
  return useAuthStore().hasPermission(
    tokenRoutingAccess.requiresPermission.resource,
    tokenRoutingAccess.requiresPermission.action
  )
})
const canWriteRouting = computed(() => {
  if (!canReadRouting.value || !getActivePinia()) return false
  return useAuthStore().hasPermission('routing', 'write')
})
const showRoutingPolicy = computed(
  () => props.editing !== null && canReadRouting.value
)

const form = reactive({
  allow_user_quota_query: true,
  name: '',
  model_limits: [] as string[],
  quotaDollars: '',
  unlimited: false,
  ipText: '',
  expireAt: '',
})
/** The datetime-local text backfilled from the token being edited. */
let loadedExpireAt = ''
let loadedQuota: { amount: string; quota: number } | null = null
let initialModelLimitsKey = ''
let savedBasePayloadKey = ''

const saving = ref(false)
const advancedOpen = ref(false)
const advancedSectionId = useId()
const baseSavedPartial = ref(false)

/** Which optional sections are expanded (hidden by default). */
const vis = reactive({
  expiry: false,
})

/* ---- Edit-mode routing policy (mode + max channel ratio) ---------------- */
const routeRequest = useLatestRequest()
const routeLoading = ref(false)
const routeLoadError = ref('')
const routeSaveError = ref('')
const routeConflict = ref(false)
const routeVersion = ref(0)
const routeEntries = ref<TokenRouteEntry[]>([])
const routeDraft = reactive({
  mode: 'auto' as TokenRouteMode,
  maxRatio: '',
  orphan: false,
})
let routeBaseline = {
  mode: 'auto' as TokenRouteMode,
  selected_only: false,
  max_channel_ratio: null as string | null,
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

function resetRoutingState(): void {
  routeRequest.cancel()
  routeLoading.value = false
  routeLoadError.value = ''
  routeSaveError.value = ''
  routeConflict.value = false
  baseSavedPartial.value = false
  savedBasePayloadKey = ''
  routeVersion.value = 0
  routeEntries.value = []
  routeDraft.mode = 'auto'
  routeDraft.maxRatio = ''
  routeDraft.orphan = false
  routeBaseline = {
    mode: 'auto',
    selected_only: false,
    max_channel_ratio: null,
  }
}

function applyLoadedRoute(route: TokenRoute): void {
  routeVersion.value = route.version
  routeEntries.value = route.entries.map((entry) => ({ ...entry }))
  routeDraft.mode = route.mode
  routeDraft.maxRatio = route.max_channel_ratio ?? ''
  routeDraft.orphan = route.selected_only && route.entries.length === 0
  routeBaseline = {
    mode: route.mode,
    selected_only: route.selected_only,
    max_channel_ratio: canonicalRatio(routeDraft.maxRatio),
  }
}

async function loadRouting(): Promise<void> {
  routeRequest.cancel()
  routeLoadError.value = ''
  routeSaveError.value = ''
  routeConflict.value = false
  if (!props.open || !props.editing || !canReadRouting.value) {
    routeLoading.value = false
    return
  }
  routeLoading.value = true
  const id = props.editing.id
  const result = await routeRequest.run((signal) =>
    tokenRoutingApi.get(id, signal)
  )
  if (result.stale) return
  routeLoading.value = false
  if (result.ok) {
    applyLoadedRoute(result.value)
  } else {
    routeLoadError.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
  }
}

const routeValidRatio = computed(() => {
  const value = routeDraft.maxRatio.trim()
  return !value || isValidChannelRatio(value)
})
const routeEnabledCount = computed(
  () => routeEntries.value.filter((entry) => entry.enabled).length
)
const routeAllPaused = computed(
  () => routeEntries.value.length > 0 && routeEnabledCount.value === 0
)
const routeEffectiveSelectedOnly = computed(
  () => routeEntries.value.length > 0 || routeDraft.orphan
)
const routeDirty = computed(() => {
  if (!showRoutingPolicy.value || !canWriteRouting.value) return false
  if (routeLoading.value || routeLoadError.value !== '') return false
  return (
    routeDraft.mode !== routeBaseline.mode ||
    routeEffectiveSelectedOnly.value !== routeBaseline.selected_only ||
    canonicalRatio(routeDraft.maxRatio) !== routeBaseline.max_channel_ratio
  )
})
const canInteractRouting = computed(
  () =>
    showRoutingPolicy.value &&
    canWriteRouting.value &&
    !saving.value &&
    !routeLoading.value &&
    !routeLoadError.value &&
    !routeConflict.value
)
const routeValidationError = computed(() => {
  if (!showRoutingPolicy.value || !canWriteRouting.value || !routeDirty.value) {
    return ''
  }
  if (!routeValidRatio.value) return t('keys.routingPanel.invalidRatio')
  if (routeDraft.orphan) return t('keys.routingPanel.orphanedRequired')
  if (routeDraft.mode === 'ordered' && routeEntries.value.length === 0) {
    return t('keys.routingPanel.orderedRequired')
  }
  return ''
})
const confirmDisabled = computed(() => {
  if (saving.value) return true
  if (!showRoutingPolicy.value || !canWriteRouting.value) return false
  if (routeConflict.value) return true
  if (!routeValidRatio.value) return true
  return routeDirty.value && routeValidationError.value !== ''
})

function setRouteMode(mode: TokenRouteMode): void {
  if (!canInteractRouting.value) return
  routeSaveError.value = ''
  routeDraft.mode = mode
}

function resetRouteToPlatform(): void {
  if (!canInteractRouting.value) return
  routeSaveError.value = ''
  routeDraft.mode = 'auto'
  routeDraft.orphan = false
}

function openChannelPicker(initialMode?: TokenRouteMode): void {
  if (!props.editing) return
  emit('open-routing', { token: props.editing, initialMode })
}

function normalizeModelLimitsKey(items: readonly string[]): string {
  return [...items]
    .map((item) => item.trim())
    .filter(Boolean)
    .sort()
    .join('\u0000')
}

watch(
  [() => props.open, () => props.editing?.id],
  ([open]) => {
    if (!open) {
      resetRoutingState()
      return
    }
    const e = props.editing
    form.allow_user_quota_query = e ? (e.allow_user_quota_query ?? false) : true
    form.name = e?.name ?? ''
    form.model_limits = e?.model_limits ? [...e.model_limits] : []
    initialModelLimitsKey = normalizeModelLimitsKey(form.model_limits)
    form.quotaDollars = e && !e.unlimited ? quotaToDecimal(e.remain_quota) : ''
    loadedQuota =
      e && !e.unlimited
        ? { amount: form.quotaDollars, quota: e.remain_quota }
        : null
    form.unlimited = e?.unlimited ?? true
    form.ipText = e?.ip_limits?.join('\n') ?? ''
    form.expireAt =
      e && e.expired_time > 0 ? toDateTimeLocal(e.expired_time) : ''
    loadedExpireAt = form.expireAt

    vis.expiry = form.expireAt.length > 0
    advancedOpen.value = Boolean(
      e &&
      (form.model_limits.length > 0 ||
        form.ipText.trim().length > 0 ||
        form.allow_user_quota_query === false)
    )
    resetRoutingState()
    if (e && canReadRouting.value) {
      void loadRouting()
    }
  },
  { immediate: true }
)

watch(canReadRouting, (readable) => {
  if (!props.open || !props.editing) return
  if (readable) void loadRouting()
  else resetRoutingState()
})

watch(
  () => routeDraft.maxRatio,
  () => {
    routeSaveError.value = ''
  }
)

onBeforeUnmount(() => {
  routeRequest.cancel()
})

/** Native expired_time: -1 never expires, otherwise epoch seconds. */
function expiredTime(): number {
  if (!vis.expiry) return -1
  // Local text carries no UTC offset, so an untouched value keeps the stored
  // instant instead of being re-parsed (a repeated DST hour would shift it).
  if (props.editing && loadedExpireAt && form.expireAt === loadedExpireAt)
    return props.editing.expired_time
  const seconds = fromDateTimeLocal(form.expireAt)
  if (seconds === null) throw new Error(t('keys.expireInvalid'))
  return seconds
}

const advancedConfiguredCount = computed(() => {
  let count = 0
  if (form.model_limits.length > 0) count++
  if (form.ipText.trim()) count++
  if (!form.allow_user_quota_query) count++
  return count
})

const createdMeta = computed(() =>
  props.editing && props.editing.created_time > 0
    ? t('keys.createdAt', { time: formatTime(props.editing.created_time) })
    : ''
)

const showModelLimitsRoutingHint = computed(() => {
  const e = props.editing
  if (!e) return false
  const isSelectedScope =
    e.route_scope === 'selected' ||
    (e.route_scope === undefined &&
      e.selected_only &&
      (e.enabled_route_channel_count ?? e.route_channel_count) > 0)
  if (!isSelectedScope) return false
  return normalizeModelLimitsKey(form.model_limits) !== initialModelLimitsKey
})

function buildPayload(): Record<string, unknown> {
  let remainQuota = 0
  if (!form.unlimited) {
    // Some runtime scales have no exact six-decimal dollar representation.
    // Editing another field must preserve the original integer budget.
    remainQuota =
      loadedQuota?.amount === form.quotaDollars
        ? loadedQuota.quota
        : Number(decimalToQuota(form.quotaDollars || '0'))
  }
  if (!Number.isSafeInteger(remainQuota) || remainQuota < 0)
    throw new Error(t('common.invalidInput'))
  return {
    name: form.name,
    allow_user_quota_query: form.allow_user_quota_query,
    model_limits_enabled: form.model_limits.length > 0,
    model_limits: form.model_limits.join(','),
    unlimited_quota: form.unlimited,
    remain_quota: remainQuota,
    allow_ips: form.ipText.trim(),
    expired_time: expiredTime(),
  }
}

async function save() {
  if (saving.value) return
  if (showRoutingPolicy.value && canWriteRouting.value && routeDirty.value) {
    if (routeConflict.value) return
    if (routeValidationError.value) {
      routeSaveError.value = routeValidationError.value
      return
    }
  }
  let payload: Record<string, unknown>
  try {
    payload = buildPayload()
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
    return
  }

  saving.value = true
  routeSaveError.value = ''
  try {
    if (props.editing) {
      payload.id = props.editing.id
      const currentBaseKey = JSON.stringify(payload)
      if (!baseSavedPartial.value || currentBaseKey !== savedBasePayloadKey) {
        await api.put('/api/token/', payload)
        savedBasePayloadKey = currentBaseKey
        baseSavedPartial.value = false
      }
      if (
        showRoutingPolicy.value &&
        canWriteRouting.value &&
        routeDirty.value
      ) {
        try {
          const selectedOnly = routeEffectiveSelectedOnly.value
          const savedRoute = await tokenRoutingApi.save(props.editing.id, {
            mode: routeDraft.mode,
            selected_only: selectedOnly,
            version: routeVersion.value,
            max_channel_ratio: routeDraft.maxRatio.trim() || null,
            entries: selectedOnly
              ? routeEntries.value.map((entry, position) => ({
                  channel_id: entry.channel_id,
                  enabled: entry.enabled,
                  position,
                }))
              : [],
          })
          applyLoadedRoute(savedRoute)
        } catch (error) {
          baseSavedPartial.value = true
          const isConflict = error instanceof ApiError && error.status === 409
          routeConflict.value = isConflict
          routeSaveError.value = isConflict
            ? t('keys.routingPanel.conflict')
            : error instanceof ApiError
              ? error.message
              : String(error)
          emit('saved')
          return
        }
      }
      toast.success(t('keys.updated'))
    } else {
      await api.post('/api/token/', payload)
      toast.success(t('keys.created'))
    }
    emit('saved')
    emit('close')
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <ConsoleModal
    :open="open"
    :title="editing ? t('keys.editTitle') : t('keys.createTitle')"
    size="lg"
    @close="emit('close')"
  >
    <div class="sibling-space-y-4 text-left">
      <p
        v-if="createdMeta"
        data-key-created-at
        class="font-mono text-xs text-[var(--text-tertiary-overlay,var(--text-tertiary))]"
      >
        {{ createdMeta }}
      </p>

      <FormField :label="t('keys.nameLabel')">
        <TextInput
          v-model="form.name"
          name="token-name"
          :maxlength="50"
          :placeholder="t('keys.namePlaceholder')"
        />
      </FormField>

      <div class="grid items-start gap-4 sm:grid-cols-2">
        <!-- expiry: toggle-hidden -->
        <div
          class="overflow-hidden rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
        >
          <div class="flex items-center justify-between px-4 py-2.5">
            <span class="text-sm font-medium text-[var(--text-secondary)]">{{
              t('keys.expireLabel')
            }}</span>
            <ConsoleToggle
              v-model="vis.expiry"
              :label="t('keys.expireLabel')"
            />
          </div>
          <div
            :inert="!vis.expiry"
            :aria-hidden="!vis.expiry"
            class="grid transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none"
            :class="vis.expiry ? 'grid-rows-[1fr]' : 'grid-rows-[0fr]'"
          >
            <div class="overflow-hidden">
              <div
                class="border-t border-[var(--border-subtle)] px-4 pt-2 pb-3"
              >
                <input
                  v-model="form.expireAt"
                  type="datetime-local"
                  step="1"
                  name="token-expiration"
                  :aria-label="t('keys.expireLabel')"
                  class="h-10 w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-3 text-sm text-[var(--text-primary)] focus-ring"
                />
              </div>
            </div>
          </div>
        </div>

        <!-- quota: unlimited by default; toggle to set a cap -->
        <div
          class="overflow-hidden rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
        >
          <div class="flex items-center justify-between px-4 py-2.5">
            <span class="text-sm font-medium text-[var(--text-secondary)]">{{
              t('billing.budgetUSD')
            }}</span>
            <label
              class="flex items-center gap-2 text-xs text-[var(--text-secondary)]"
            >
              <ConsoleToggle
                v-model="form.unlimited"
                :label="t('keys.unlimitedQuota')"
              />
              {{ t('keys.unlimitedQuota') }}
            </label>
          </div>
          <div
            :inert="form.unlimited"
            :aria-hidden="form.unlimited"
            class="grid transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none"
            :class="!form.unlimited ? 'grid-rows-[1fr]' : 'grid-rows-[0fr]'"
          >
            <div class="overflow-hidden">
              <div
                class="border-t border-[var(--border-subtle)] px-4 pt-2 pb-3"
              >
                <input
                  v-model="form.quotaDollars"
                  inputmode="decimal"
                  class="h-10 w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-3 font-mono text-sm text-[var(--text-primary)] focus-ring"
                  name="token-quota"
                  :aria-label="t('billing.budgetUSD')"
                  placeholder="20"
                />
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Routing policy (mode + multiplier cap), edit mode only -->
      <div
        v-if="showRoutingPolicy"
        data-key-form-routing
        class="sibling-space-y-3"
      >
        <div
          v-if="routeSaveError"
          role="alert"
          data-routing-save-error
          class="no-handdrawn flex flex-wrap items-center justify-between gap-3 rounded-xl border p-3 text-xs"
          :class="
            routeConflict
              ? 'border-[var(--status-warning)] bg-[var(--status-warning-soft)] text-[var(--status-warning-text)]'
              : 'border-[var(--status-danger)] bg-[var(--status-danger-soft)] text-[var(--status-danger-text)]'
          "
        >
          <div class="min-w-0 flex-1 space-y-1 leading-relaxed">
            <p v-if="baseSavedPartial">
              {{ t('keys.routingPanel.partialSavedNotice') }}
            </p>
            <p>{{ routeSaveError }}</p>
          </div>
          <ConsoleButton
            v-if="routeConflict"
            variant="secondary"
            size="sm"
            @click="loadRouting"
          >
            <RefreshCw :size="14" aria-hidden="true" />
            {{ t('keys.routingPanel.reload') }}
          </ConsoleButton>
        </div>

        <p
          v-if="routeLoading"
          role="status"
          class="py-3 text-center text-xs text-[var(--text-tertiary-overlay,var(--text-tertiary))]"
        >
          {{ t('keys.routingPanel.loading') }}
        </p>
        <div
          v-else-if="routeLoadError"
          role="alert"
          class="no-handdrawn flex flex-wrap items-center justify-between gap-3 rounded-xl border border-[var(--status-danger)] bg-[var(--status-danger-soft)] p-3 text-xs text-[var(--status-danger-text)]"
        >
          <span class="min-w-0 flex-1">{{
            t('keys.routingPanel.loadFailed', { error: routeLoadError })
          }}</span>
          <ConsoleButton variant="secondary" size="sm" @click="loadRouting">
            <RefreshCw :size="14" aria-hidden="true" />
            {{ t('keys.routingPanel.reload') }}
          </ConsoleButton>
        </div>

        <template v-else>
          <div
            v-if="!canWriteRouting"
            data-routing-readonly
            class="no-handdrawn flex items-center gap-2.5 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-3.5 py-2.5 text-xs text-[var(--text-secondary)]"
          >
            <Lock :size="14" class="shrink-0" aria-hidden="true" />
            <span>{{ t('keys.routingPanel.readOnlyEdit') }}</span>
          </div>

          <div
            v-if="routeDraft.orphan"
            role="alert"
            data-route-orphaned
            class="no-handdrawn flex flex-col gap-2.5 rounded-xl border border-[var(--status-warning)] bg-[var(--status-warning-soft)] p-3.5 text-xs text-[var(--status-warning-text)]"
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
            <div v-if="canWriteRouting" class="flex flex-wrap gap-2">
              <ConsoleButton
                variant="secondary"
                size="sm"
                :disabled="!canInteractRouting"
                @click="openChannelPicker()"
              >
                {{ t('keys.routingPanel.goSelectChannels') }}
              </ConsoleButton>
              <ConsoleButton
                variant="secondary"
                size="sm"
                :disabled="!canInteractRouting"
                @click="resetRouteToPlatform"
              >
                {{ t('keys.routingPanel.usePlatform') }}
              </ConsoleButton>
            </div>
          </div>

          <fieldset :disabled="!canInteractRouting" class="min-w-0">
            <legend
              class="mb-2 text-xs font-medium text-[var(--text-secondary)]"
            >
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
                :aria-checked="routeDraft.mode === mode"
                :disabled="!canInteractRouting"
                class="no-handdrawn flex min-h-[44px] flex-col rounded-xl border px-3.5 py-2.5 text-left transition-colors focus-ring disabled:cursor-not-allowed disabled:opacity-60"
                :class="
                  routeDraft.mode === mode
                    ? 'border-[var(--accent)] bg-[var(--accent-soft)]'
                    : 'border-[var(--border-subtle)] bg-[var(--surface-solid)] hover:border-[var(--border-strong)]'
                "
                @click="setRouteMode(mode)"
              >
                <span class="flex w-full items-center justify-between gap-2">
                  <span
                    class="text-sm font-semibold"
                    :class="
                      routeDraft.mode === mode
                        ? 'text-[var(--accent-text)]'
                        : 'text-[var(--text-primary)]'
                    "
                  >
                    {{ t(`keys.routingPanel.modes.${mode}`) }}
                  </span>
                  <span
                    v-if="mode === 'ordered' && routeAllPaused"
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

          <div
            v-if="
              routeDraft.mode === 'ordered' &&
              routeEntries.length === 0 &&
              !routeDraft.orphan
            "
            role="alert"
            data-routing-ordered-empty
            class="no-handdrawn flex flex-wrap items-center justify-between gap-2 rounded-xl border border-[var(--status-warning)] bg-[var(--status-warning-soft)] px-3.5 py-2.5 text-xs text-[var(--status-warning-text)]"
          >
            <span>{{ t('keys.routingPanel.orderedRequired') }}</span>
            <div v-if="canInteractRouting" class="flex flex-wrap gap-2">
              <button
                type="button"
                class="min-h-[44px] rounded-lg px-2 py-1 font-medium underline underline-offset-2 focus-ring sm:min-h-0 sm:px-0 sm:py-0"
                data-routing-go-select
                @click="openChannelPicker('ordered')"
              >
                {{ t('keys.routingPanel.goSelectChannels') }}
              </button>
              <button
                type="button"
                class="min-h-[44px] rounded-lg px-2 py-1 underline underline-offset-2 focus-ring sm:min-h-0 sm:px-0 sm:py-0"
                @click="resetRouteToPlatform"
              >
                {{ t('keys.routingPanel.switchToAuto') }}
              </button>
            </div>
          </div>

          <div
            v-if="routeAllPaused"
            role="status"
            data-routing-all-paused
            class="no-handdrawn flex items-start gap-2 rounded-xl border border-[var(--status-info)] bg-[var(--status-info-soft)] p-3 text-xs text-[var(--status-info-text)]"
          >
            <Info :size="15" class="mt-0.5 shrink-0" aria-hidden="true" />
            <div class="min-w-0 flex-1 space-y-1">
              <p class="leading-relaxed">
                {{ t('keys.routingPanel.allPausedNotice') }}
              </p>
              <p v-if="routeDraft.mode === 'ordered'" class="opacity-85">
                {{ t('keys.routingPanel.orderedInactiveHint') }}
              </p>
            </div>
          </div>

          <div
            v-if="canWriteRouting || routeDraft.maxRatio"
            class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3.5 py-3"
          >
            <FormField
              :label="t('keys.routingPanel.maxRatio')"
              :hint="t('keys.routingPanel.maxRatioHint')"
            >
              <TextInput
                v-model="routeDraft.maxRatio"
                name="token-max-channel-ratio"
                inputmode="decimal"
                maxlength="24"
                :aria-label="t('keys.routingPanel.maxRatio')"
                :aria-invalid="!routeValidRatio"
                :placeholder="t('keys.routingPanel.maxRatioPlaceholder')"
                :disabled="!canInteractRouting"
              />
            </FormField>
            <p
              v-if="!routeValidRatio"
              role="alert"
              class="mt-1.5 text-xs text-[var(--status-danger-text)]"
            >
              {{ t('keys.routingPanel.invalidRatio') }}
            </p>
          </div>
        </template>
      </div>

      <section
        class="overflow-hidden rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
      >
        <button
          type="button"
          class="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-[var(--surface-muted)] focus-ring"
          :aria-expanded="advancedOpen"
          :aria-controls="advancedSectionId"
          @click="advancedOpen = !advancedOpen"
        >
          <span
            class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-[var(--accent-soft)] text-[var(--accent-text)]"
            aria-hidden="true"
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
            >
              <path d="M4 7h10M18 7h2M4 17h2M10 17h10" />
              <circle cx="16" cy="7" r="2" />
              <circle cx="8" cy="17" r="2" />
            </svg>
          </span>
          <span class="min-w-0 flex-1">
            <span
              class="block text-sm font-semibold text-[var(--text-primary)]"
            >
              {{ t('keys.advancedTitle') }}
            </span>
            <span class="block truncate text-xs text-[var(--text-tertiary)]">
              {{ t('keys.advancedSummary') }}
            </span>
          </span>
          <span
            v-if="advancedConfiguredCount > 0"
            class="shrink-0 rounded-full bg-[var(--accent-soft)] px-2 py-0.5 text-xs font-medium text-[var(--accent-text)]"
          >
            {{
              t('keys.advancedConfigured', { count: advancedConfiguredCount })
            }}
          </span>
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            class="shrink-0 text-[var(--text-tertiary)] transition-transform"
            :class="{ 'rotate-180': advancedOpen }"
            aria-hidden="true"
          >
            <path d="m6 9 6 6 6-6" />
          </svg>
        </button>

        <div
          v-if="advancedOpen"
          :id="advancedSectionId"
          class="sibling-space-y-4 border-t border-[var(--border-subtle)] bg-[var(--surface-muted)] px-4 py-4"
        >
          <div
            class="flex items-start justify-between gap-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-4 py-3"
          >
            <div class="min-w-0">
              <label
                :for="advancedSectionId + '-quota-toggle'"
                class="cursor-pointer text-sm font-medium text-[var(--text-secondary)]"
              >
                {{ t('keys.allowUserQuotaQuery') }}
              </label>
              <p
                class="mt-1 text-xs leading-relaxed text-[var(--text-tertiary)]"
              >
                {{ t('keys.allowUserQuotaQueryHint') }}
              </p>
            </div>
            <ConsoleToggle
              :id="advancedSectionId + '-quota-toggle'"
              v-model="form.allow_user_quota_query"
              name="token-allow-user-quota-query"
              :label="t('keys.allowUserQuotaQuery')"
              :disabled="saving"
            />
          </div>

          <div
            class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-4 py-3"
          >
            <FormField group :label="t('keys.modelsLabel')">
              <ChipPicker
                v-model="form.model_limits"
                :options="models"
                :placeholder="t('keys.modelsPlaceholder')"
              />
            </FormField>
            <p
              v-if="showModelLimitsRoutingHint"
              role="status"
              data-model-limits-routing-hint
              class="no-handdrawn mt-2.5 rounded-lg border border-[var(--status-warning)] bg-[var(--status-warning-soft)] px-3 py-2 text-xs leading-relaxed text-[var(--status-warning-text)]"
            >
              {{
                t('keys.modelLimitsRoutingHint', {
                  count: editing?.route_channel_count ?? 0,
                })
              }}
            </p>
          </div>

          <div
            class="rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-4 py-3"
          >
            <FormField :label="t('keys.ipLabel')">
              <textarea
                v-model="form.ipText"
                rows="2"
                name="token-ip-limits"
                :aria-label="t('keys.ipLabel')"
                :placeholder="t('keys.ipPlaceholder')"
                class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-4 py-2.5 font-mono text-sm text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] transition-colors focus:border-[var(--border-strong)] focus-ring"
              />
            </FormField>
          </div>
        </div>
      </section>
    </div>

    <template #footer>
      <div class="grid grid-cols-2 gap-3">
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
          :disabled="confirmDisabled"
          @click="save"
        >
          {{ t('common.confirm') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>
