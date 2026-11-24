<script setup lang="ts">
import { computed, nextTick, reactive, ref, useId, watch } from 'vue'
import { getActivePinia } from 'pinia'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import { parseCreatedTokenId, tokenRoutingApi } from '@/api/tokenRouting'
import { ApiError } from '@/api/types'
import type { TokenSummary } from '@/types/console'
import ChipPicker from '@/components/common/ChipPicker.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import KeyRoutingSection from '@/components/console/keys/KeyRoutingSection.vue'
import { useToast } from '@/composables/useToast'
import { tokenRoutingAccess } from '@/constants/navigation/consoleNav'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { fromDateTimeLocal, toDateTimeLocal } from '@/utils/format'
import { decimalToQuota, quotaToDecimal } from '@/utils/quota'

const props = withDefaults(
  defineProps<{
    open: boolean
    /** null = create mode */
    editing: TokenSummary | null
    models: string[]
    focus?: 'routing' | null
  }>(),
  { focus: null }
)

const emit = defineEmits<{
  close: []
  saved: []
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

const routingRef = ref<InstanceType<typeof KeyRoutingSection> | null>(null)

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
let baseBaseline = ''

const saving = ref(false)
const advancedOpen = ref(false)
const advancedSectionId = useId()

/** Tracks a token created during step 1 when step 2 routing PUT failed. */
const createdTokenId = ref<number | null>(null)
const routingSaveError = ref('')
const routingConflict = ref(false)
const routingPartialNotice = ref('')

const effectiveTokenId = computed(
  () => props.editing?.id ?? createdTokenId.value
)

/** Which optional sections are expanded (hidden by default). */
const vis = reactive({
  expiry: false,
})

function baseSnapshotKey(): string {
  return JSON.stringify({
    name: form.name,
    allow_user_quota_query: form.allow_user_quota_query,
    model_limits: form.model_limits,
    unlimited: form.unlimited,
    quotaDollars: form.unlimited ? '' : form.quotaDollars,
    ipText: form.ipText.trim(),
    expiry: vis.expiry,
    expireAt: vis.expiry ? form.expireAt : '',
  })
}

function clearRoutingErrors(): void {
  routingSaveError.value = ''
  routingConflict.value = false
  routingPartialNotice.value = ''
}

watch(
  () => props.open,
  (open) => {
    if (!open) return
    const e = props.editing
    createdTokenId.value = null
    clearRoutingErrors()
    form.allow_user_quota_query = e ? (e.allow_user_quota_query ?? false) : true
    form.name = e?.name ?? ''
    form.model_limits = e?.model_limits ? [...e.model_limits] : []
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
    baseBaseline = baseSnapshotKey()
    if (props.focus === 'routing' && canReadRouting.value) {
      void nextTick(() => routingRef.value?.scrollIntoView())
    }
  },
  { immediate: true }
)

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

function buildBasePayload(): Record<string, unknown> {
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

async function saveRoutingOnly(
  tokenId: number,
  stage: 'created' | 'updated' | 'standalone'
): Promise<boolean> {
  const routingState = routingRef.value?.getState()
  if (!routingState || !canWriteRouting.value) return true
  if (routingState.validationError) {
    routingSaveError.value = routingState.validationError
    return false
  }
  try {
    const saved = await tokenRoutingApi.save(tokenId, routingState.payload)
    routingRef.value?.markSaved(saved)
    clearRoutingErrors()
    return true
  } catch (error) {
    const message = error instanceof ApiError ? error.message : String(error)
    const isConflict = error instanceof ApiError && error.status === 409
    routingConflict.value = isConflict
    if (isConflict) {
      routingSaveError.value = t('keys.routingPanel.conflict')
      routingPartialNotice.value = ''
    } else if (stage === 'created') {
      routingPartialNotice.value = t('keys.routingPanel.partialCreated', {
        error: message,
      })
      routingSaveError.value = ''
    } else if (stage === 'updated') {
      routingPartialNotice.value = t('keys.routingPanel.partialUpdated', {
        error: message,
      })
      routingSaveError.value = ''
    } else {
      routingSaveError.value = message
      routingPartialNotice.value = ''
    }
    return false
  }
}

async function retryRouting(): Promise<void> {
  if (saving.value || effectiveTokenId.value === null) return
  saving.value = true
  try {
    const ok = await saveRoutingOnly(
      effectiveTokenId.value,
      createdTokenId.value !== null ? 'created' : 'standalone'
    )
    if (!ok) return
    toast.success(props.editing ? t('keys.updated') : t('keys.created'))
    emit('saved')
    emit('close')
  } finally {
    saving.value = false
  }
}

async function save() {
  if (saving.value) return
  const routingState = canWriteRouting.value
    ? routingRef.value?.getState()
    : undefined
  if (routingState?.loading) return
  if (routingState?.validationError) {
    routingSaveError.value = routingState.validationError
    return
  }

  saving.value = true
  clearRoutingErrors()
  try {
    const baseDirty = baseSnapshotKey() !== baseBaseline
    const routingDirty = Boolean(routingState?.dirty)

    // Create flow (or retry after partial create)
    if (props.editing === null) {
      let tokenId = createdTokenId.value
      const justCreated = tokenId === null
      if (justCreated) {
        const payload = buildBasePayload()
        const response = await api.post<unknown>('/api/token/', payload)
        tokenId = parseCreatedTokenId(response)
        baseBaseline = baseSnapshotKey()
      } else if (baseDirty) {
        const payload = buildBasePayload()
        payload.id = tokenId
        await api.put('/api/token/', payload)
        baseBaseline = baseSnapshotKey()
      }

      const needsRoutingSave =
        Boolean(routingState) &&
        canWriteRouting.value &&
        (!routingState!.isDefault || routingDirty)
      if (needsRoutingSave) {
        if (tokenId === null) {
          throw new Error(t('common.failed'))
        }
        createdTokenId.value = tokenId
        const ok = await saveRoutingOnly(tokenId, 'created')
        if (!ok) {
          emit('saved')
          return
        }
      }

      toast.success(t('keys.created'))
      emit('saved')
      emit('close')
      return
    }

    // Edit flow: save base fields first so backend model_limits match routing PUT.
    const tokenId = props.editing.id
    let baseSaved = false
    if (baseDirty || !routingDirty) {
      const payload = buildBasePayload()
      payload.id = tokenId
      await api.put('/api/token/', payload)
      baseBaseline = baseSnapshotKey()
      baseSaved = true
    }

    if (routingDirty && canWriteRouting.value) {
      const ok = await saveRoutingOnly(
        tokenId,
        baseSaved ? 'updated' : 'standalone'
      )
      if (!ok) {
        if (baseSaved) emit('saved')
        return
      }
    }

    toast.success(t('keys.updated'))
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
    :title="
      editing || createdTokenId !== null
        ? t('keys.editTitle')
        : t('keys.createTitle')
    "
    size="lg"
    @close="emit('close')"
  >
    <div class="sibling-space-y-4 text-left">
      <FormField :label="t('keys.nameLabel')">
        <TextInput
          v-model="form.name"
          name="token-name"
          :maxlength="50"
          :placeholder="t('keys.namePlaceholder')"
        />
      </FormField>

      <div class="grid gap-4 sm:grid-cols-2 items-start">
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
                class="border-t border-[var(--border-subtle)] px-4 pb-3 pt-2"
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
                class="border-t border-[var(--border-subtle)] px-4 pb-3 pt-2"
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

      <KeyRoutingSection
        ref="routingRef"
        :open="open"
        :editing-id="editing?.id ?? null"
        :created-token-id="createdTokenId"
        :model-limits="form.model_limits"
        :can-read="canReadRouting"
        :can-write="canWriteRouting"
        :auto-expand="focus === 'routing'"
        :disabled="saving"
        :save-error="routingSaveError"
        :conflict="routingConflict"
        :partial-notice="routingPartialNotice"
        @clear-error="clearRoutingErrors"
        @retry-routing="retryRouting"
      />

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
          :disabled="!form.name.trim() || routingConflict"
          @click="save"
        >
          {{ t('common.confirm') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>
