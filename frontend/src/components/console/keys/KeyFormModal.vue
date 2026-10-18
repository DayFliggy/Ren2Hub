<script setup lang="ts">
import TextInput from '@/components/common/TextInput.vue'
import { computed, reactive, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import type { TokenSummary, TokenType } from '@/types/console'
import ChipPicker from '@/components/common/ChipPicker.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import FormField from '@/components/common/FormField.vue'
import { useToast } from '@/composables/useToast'
import { formatDate } from '@/utils/format'
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
}>()

const { t } = useI18n()
const toast = useToast()
const form = reactive({
  type: 'auto' as TokenType,
  allow_user_quota_query: true,
  name: '',
  model_limits: [] as string[],
  quotaDollars: '',
  unlimited: false,
  ipText: '',
  expireDate: '',
})
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
    form.type = e?.type ?? 'auto'
    form.allow_user_quota_query = e ? (e.allow_user_quota_query ?? false) : true
    form.name = e?.name ?? ''
    form.model_limits = e?.model_limits ? [...e.model_limits] : []
    form.quotaDollars = e && !e.unlimited ? quotaToDecimal(e.remain_quota) : ''
    form.unlimited = e?.unlimited ?? true
    form.ipText = e?.ip_limits?.join('\n') ?? ''
    form.expireDate = e && e.expired_time > 0 ? formatDate(e.expired_time) : ''

    vis.expiry = form.expireDate.length > 0
    advancedOpen.value = Boolean(
      e && (form.model_limits.length > 0 || form.ipText.trim().length > 0)
    )
  },
  { immediate: true }
)

const typeCards = computed(() => [
  {
    value: 'manual' as const,
    title: t('keys.type.manual'),
    desc: t('keys.type.manualDesc'),
  },
  {
    value: 'auto' as const,
    title: t('keys.type.auto'),
    desc: t('keys.type.autoDesc'),
  },
])

const advancedConfiguredCount = computed(() => {
  let count = 0
  if (form.model_limits.length > 0) count++
  if (form.ipText.trim()) count++
  return count
})

async function save() {
  if (saving.value) return
  saving.value = true
  try {
    const remainQuota = form.unlimited
      ? 0
      : Number(decimalToQuota(form.quotaDollars || '0'))
    if (!Number.isSafeInteger(remainQuota) || remainQuota < 0)
      throw new Error(t('common.invalidInput'))
    const payload: Record<string, unknown> = {
      name: form.name,
      allow_user_quota_query: form.allow_user_quota_query,
      model_limits: form.model_limits,
      unlimited: form.unlimited,
      remain_quota: remainQuota,
      ip_limits: form.ipText
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean),
      // The date is a local calendar day, as the key list shows it; a bare
      // YYYY-MM-DD would parse as UTC midnight.
      expired_time:
        vis.expiry && form.expireDate
          ? Math.floor(new Date(`${form.expireDate}T00:00`).getTime() / 1000)
          : -1,
    }
    {
      payload.unlimited_quota = payload.unlimited
      payload.model_limits_enabled = form.model_limits.length > 0
      payload.model_limits = form.model_limits.join(',')
      payload.allow_ips = form.ipText.trim()
      delete payload.unlimited
      delete payload.ip_limits
    }
    if (props.editing) {
      payload.id = props.editing.id
      await api.put('/api/token/', payload)
      toast.success(t('keys.updated'))
    } else {
      payload.type = form.type
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
      <!-- Token type is fixed after creation. -->
      <fieldset class="min-w-0">
        <legend class="mb-1.5 text-sm font-medium text-[var(--text-secondary)]">
          {{ t('keys.type.label') }}
        </legend>
        <div
          class="grid gap-2 sm:grid-cols-2"
          role="radiogroup"
          :aria-label="t('keys.type.label')"
        >
          <button
            v-for="card in typeCards"
            :key="card.value"
            type="button"
            role="radio"
            :aria-checked="form.type === card.value"
            :disabled="editing !== null"
            class="rounded-xl border px-3.5 py-3 text-left transition-colors focus-ring disabled:cursor-not-allowed"
            :class="
              form.type === card.value
                ? 'border-[var(--accent)] bg-[var(--accent-soft)]'
                : 'border-[var(--border-subtle)] bg-[var(--surface-solid)] hover:border-[var(--border-strong)] disabled:opacity-50'
            "
            @click="form.type = card.value"
          >
            <span
              class="block text-sm font-semibold"
              :class="
                form.type === card.value
                  ? 'text-[var(--accent-text)]'
                  : 'text-[var(--text-primary)]'
              "
            >
              {{ card.title }}
            </span>
            <span
              class="mt-0.5 block text-xs leading-relaxed text-[var(--text-secondary)]"
              >{{ card.desc }}</span
            >
          </button>
        </div>
      </fieldset>
      <FormField :label="t('keys.nameLabel')"
        ><TextInput v-model="form.name" name="token-name" :maxlength="50"
      /></FormField>

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
            v-if="vis.expiry"
            class="border-t border-[var(--border-subtle)] px-4 pb-3 pt-2"
          >
            <input
              v-model="form.expireDate"
              type="date"
              name="token-expiration"
              :aria-label="t('keys.expireLabel')"
              class="h-10 w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-3 text-sm text-[var(--text-primary)] focus-ring"
            />
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
            v-if="!form.unlimited"
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

      <div
        class="flex items-start justify-between gap-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-4 py-3"
      >
        <div class="min-w-0">
          <p class="text-sm font-medium text-[var(--text-secondary)]">
            {{ t('keys.allowUserQuotaQuery') }}
          </p>
          <p class="mt-1 text-xs leading-relaxed text-[var(--text-tertiary)]">
            {{ t('keys.allowUserQuotaQueryHint') }}
          </p>
        </div>
        <ConsoleToggle
          v-model="form.allow_user_quota_query"
          name="token-allow-user-quota-query"
          :label="t('keys.allowUserQuotaQuery')"
          :disabled="saving"
        />
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
          <FormField :label="t('keys.modelsLabel')">
            <ChipPicker
              v-model="form.model_limits"
              :options="models"
              :placeholder="t('keys.modelsPlaceholder')"
            />
          </FormField>

          <FormField :label="t('keys.ipLabel')">
            <textarea
              v-model="form.ipText"
              rows="2"
              name="token-ip-limits"
              :aria-label="t('keys.ipLabel')"
              :placeholder="t('keys.ipPlaceholder')"
              class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-4 py-2.5 font-mono text-sm text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] transition-colors focus:border-[var(--border-strong)] focus-ring"
            />
          </FormField>
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
