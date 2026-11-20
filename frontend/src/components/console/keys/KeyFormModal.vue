<script setup lang="ts">
import TextInput from '@/components/common/TextInput.vue'
import { computed, reactive, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import type { TokenSummary } from '@/types/console'
import ChipPicker from '@/components/common/ChipPicker.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import FormField from '@/components/common/FormField.vue'
import { useToast } from '@/composables/useToast'
import { fromDateTimeLocal, toDateTimeLocal } from '@/utils/format'
import { decimalToQuota, quotaToDecimal } from '@/utils/quota'

const props = defineProps<{
  open: boolean
  /** null = create mode */
  editing: TokenSummary | null
  models: string[]
}>()

const emit = defineEmits<{
  close: []
  /** created=true after a new key; routing is configured on its own page. */
  saved: [created: boolean]
}>()

const { t } = useI18n()
const toast = useToast()
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
const saving = ref(false)
const advancedOpen = ref(false)
const advancedSectionId = useId()

/** Which optional sections are expanded (hidden by default). */
const vis = reactive({
  expiry: false,
})

watch(
  () => props.open,
  (open) => {
    if (!open) return
    const e = props.editing
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

async function save() {
  if (saving.value) return
  saving.value = true
  try {
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
    const payload: Record<string, unknown> = {
      name: form.name,
      allow_user_quota_query: form.allow_user_quota_query,
      model_limits_enabled: form.model_limits.length > 0,
      model_limits: form.model_limits.join(','),
      unlimited_quota: form.unlimited,
      remain_quota: remainQuota,
      allow_ips: form.ipText.trim(),
      expired_time: expiredTime(),
    }
    if (props.editing) {
      payload.id = props.editing.id
      await api.put('/api/token/', payload)
      toast.success(t('keys.updated'))
    } else {
      await api.post('/api/token/', payload)
      toast.success(t('keys.created'))
    }
    emit('saved', !props.editing)
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
          :disabled="!form.name.trim()"
          @click="save"
        >
          {{ t('common.confirm') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>
