<script setup lang="ts">
import { RefreshCw } from 'lucide-vue-next'
import { computed, onScopeDispose, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  affiliateApi,
  type AffiliatePolicyUpdate,
  type AffiliateUserPolicy,
} from '@/api/affiliate'
import { ApiError } from '@/api/types'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useToast } from '@/composables/useToast'
import type { AdminUser } from '@/types/console'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { formatQuota } from '@/utils/format'

const props = defineProps<{
  open: boolean
  target: AdminUser | null
}>()

const emit = defineEmits<{ close: []; saved: [] }>()

const { t } = useI18n()
const toast = useToast()
const loading = ref(false)
const saving = ref(false)
const resetting = ref(false)
const detail = ref<AffiliateUserPolicy | null>(null)
const form = reactive({
  code: '',
  codeEnabled: true,
  useCustomRate: false,
  ratePercent: 10,
})

const codeValid = computed(() => /^[A-Z0-9_-]{4,32}$/.test(form.code.trim()))
const rateValid = computed(
  () =>
    !form.useCustomRate ||
    (Number.isFinite(form.ratePercent) &&
      form.ratePercent >= 0 &&
      form.ratePercent <= 100)
)
const valid = computed(() => codeValid.value && rateValid.value)
const policyRequest = useLatestRequest()
let mutationSequence = 0
onScopeDispose(() => {
  mutationSequence += 1
})

function applyDetail(value: AffiliateUserPolicy) {
  detail.value = value
  form.code = value.code
  form.codeEnabled = value.enabled
  form.useCustomRate = value.rate_bps !== null
  form.ratePercent = (value.rate_bps ?? value.effective_rate_bps) / 100
}

async function load() {
  if (!props.target) return
  const id = props.target.id
  loading.value = true
  const result = await policyRequest.run((signal) =>
    affiliateApi.userPolicy(id, signal)
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    toast.error(
      result.error instanceof ApiError
        ? result.error.message
        : String(result.error)
    )
    emit('close')
    return
  }
  applyDetail(result.value)
}

watch(
  () => [props.open, props.target?.id] as const,
  ([open]) => {
    mutationSequence += 1
    policyRequest.cancel()
    detail.value = null
    saving.value = false
    resetting.value = false
    if (open) void load()
  },
  { immediate: true }
)

function close() {
  if (!saving.value && !resetting.value) emit('close')
}

async function submit() {
  if (
    !props.target ||
    !detail.value ||
    !props.open ||
    !valid.value ||
    saving.value ||
    resetting.value
  )
    return
  const id = props.target.id
  const sequence = ++mutationSequence
  const input: AffiliatePolicyUpdate = {
    enabled: form.codeEnabled,
    rate_bps: form.useCustomRate ? Math.round(form.ratePercent * 100) : null,
  }
  if (form.code.trim() !== detail.value.code) input.code = form.code.trim()
  saving.value = true
  try {
    await affiliateApi.updateUserPolicy(id, input)
    if (sequence !== mutationSequence || props.target?.id !== id) return
    toast.success(t('users.affiliateSaved'))
    emit('saved')
    emit('close')
  } catch (error) {
    if (sequence === mutationSequence)
      toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    if (sequence === mutationSequence) saving.value = false
  }
}

async function resetCode() {
  if (
    !props.target ||
    !detail.value ||
    !props.open ||
    resetting.value ||
    saving.value
  )
    return
  const id = props.target.id
  const sequence = ++mutationSequence
  resetting.value = true
  try {
    const result = await affiliateApi.updateUserPolicy(id, {
      rate_bps: detail.value.rate_bps,
      reset_code: true,
    })
    if (sequence !== mutationSequence || props.target?.id !== id) return
    applyDetail(result)
    toast.success(t('users.affiliateCodeReset'))
    emit('saved')
  } catch (error) {
    if (sequence === mutationSequence)
      toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    if (sequence === mutationSequence) resetting.value = false
  }
}
</script>

<template>
  <ConsoleModal
    :open="open"
    :title="t('users.affiliateTitle')"
    :subtitle="target?.username ?? ''"
    size="lg"
    @close="close"
  >
    <div v-if="loading || !detail" class="sibling-space-y-4" aria-hidden="true">
      <div class="h-20 animate-pulse rounded-sm bg-[var(--surface-muted)]" />
      <div class="h-11 animate-pulse rounded-sm bg-[var(--surface-muted)]" />
      <div class="h-28 animate-pulse rounded-sm bg-[var(--surface-muted)]" />
    </div>

    <div v-else class="sibling-space-y-5 text-left">
      <dl class="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <div class="border-b border-[var(--border-subtle)] pb-2">
          <dt class="text-xs text-[var(--text-tertiary)]">
            {{ t('users.affiliateEffectiveRate') }}
          </dt>
          <dd
            class="mt-1 font-semibold tabular-nums text-[var(--text-primary)]"
          >
            {{ detail.effective_rate_bps / 100 }}%
          </dd>
        </div>
        <div class="border-b border-[var(--border-subtle)] pb-2">
          <dt class="text-xs text-[var(--text-tertiary)]">
            {{ t('users.affiliateInvited') }}
          </dt>
          <dd
            class="mt-1 font-semibold tabular-nums text-[var(--text-primary)]"
          >
            {{ target?.aff_count ?? 0 }}
          </dd>
        </div>
        <div class="border-b border-[var(--border-subtle)] pb-2">
          <dt class="text-xs text-[var(--text-tertiary)]">
            {{ t('users.affiliateAvailable') }}
          </dt>
          <dd
            class="mt-1 font-semibold tabular-nums text-[var(--text-primary)]"
          >
            {{ formatQuota(target?.aff_quota ?? 0) }}
          </dd>
        </div>
        <div class="border-b border-[var(--border-subtle)] pb-2">
          <dt class="text-xs text-[var(--text-tertiary)]">
            {{ t('users.affiliateFrozen') }}
          </dt>
          <dd
            class="mt-1 font-semibold tabular-nums text-[var(--text-primary)]"
          >
            {{ formatQuota(target?.aff_frozen_quota ?? 0) }}
          </dd>
        </div>
      </dl>

      <FormField
        :label="t('users.affiliateCode')"
        :hint="t('users.affiliateCodeHint')"
      >
        <div class="flex items-center gap-2">
          <TextInput
            v-model="form.code"
            name="admin-user-affiliate-code"
            autocomplete="off"
            class="min-w-0 flex-1 font-mono"
            @update:model-value="form.code = form.code.toUpperCase()"
          />
          <ConsoleButton
            variant="secondary"
            :loading="resetting"
            :disabled="saving"
            @click="resetCode"
          >
            <RefreshCw v-if="!resetting" :size="15" />
            {{ t('users.affiliateResetCode') }}
          </ConsoleButton>
        </div>
      </FormField>

      <div class="flex items-center justify-between gap-4">
        <div>
          <p class="text-sm font-medium text-[var(--text-secondary)]">
            {{ t('users.affiliateCodeEnabled') }}
          </p>
          <p class="mt-1 text-xs text-[var(--text-tertiary)]">
            {{ t('users.affiliateCodeEnabledHint') }}
          </p>
        </div>
        <ConsoleToggle
          v-model="form.codeEnabled"
          :label="t('users.affiliateCodeEnabled')"
        />
      </div>

      <div class="grid gap-4 sm:grid-cols-2">
        <div class="sibling-space-y-3">
          <div class="flex items-center justify-between gap-3">
            <div>
              <p class="text-sm font-medium text-[var(--text-secondary)]">
                {{ t('users.affiliateCustomRate') }}
              </p>
              <p class="mt-1 text-xs text-[var(--text-tertiary)]">
                {{ t('users.affiliateCustomRateHint') }}
              </p>
            </div>
            <ConsoleToggle
              v-model="form.useCustomRate"
              :label="t('users.affiliateCustomRate')"
            />
          </div>
          <input
            v-if="form.useCustomRate"
            v-model.number="form.ratePercent"
            type="number"
            min="0"
            max="100"
            step="0.01"
            class="affiliate-number focus-ring"
            :aria-label="t('users.affiliateRatePercent')"
          />
        </div>
      </div>
    </div>

    <template #footer>
      <div class="grid grid-cols-2 gap-3">
        <ConsoleButton
          variant="secondary"
          size="lg"
          :disabled="saving || resetting"
          @click="close"
        >
          {{ t('common.cancel') }}
        </ConsoleButton>
        <ConsoleButton
          size="lg"
          :loading="saving"
          :disabled="loading || !detail || !valid || resetting"
          @click="submit"
        >
          {{ t('users.affiliateSave') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>

<style scoped>
.affiliate-number {
  width: 100%;
  height: 2.75rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  background: var(--surface-solid);
  padding: 0 0.875rem;
  color: var(--text-primary);
  font-size: 0.875rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  outline: none;
}
</style>
