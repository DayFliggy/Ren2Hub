<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import FormField from '@/components/common/FormField.vue'
import type { AdminUser } from '@/types/console'
import { formatQuota } from '@/utils/format'
import { addQuota, decimalToQuota } from '@/utils/quota'

const props = defineProps<{
  open: boolean
  target: AdminUser | null
  save: (delta: string, reason: string, operationId: string) => Promise<boolean>
}>()

const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()
const saving = ref(false)
const mode = ref<'grant' | 'deduct'>('grant')
const amount = ref('1')
let operation: { delta: string; id: string } | null = null

/** Admins think in dollars; the ledger stores quota units. */
const units = computed(() => {
  try {
    return decimalToQuota(amount.value)
  } catch {
    return null
  }
})
const delta = computed(() =>
  units.value === null
    ? '0'
    : (BigInt(units.value) * (mode.value === 'grant' ? 1n : -1n)).toString()
)
const nextQuota = computed(() => {
  try {
    return addQuota(String(props.target?.quota ?? 0), delta.value)
  } catch {
    return null
  }
})
const valid = computed(
  () =>
    units.value !== null &&
    BigInt(units.value) > 0n &&
    nextQuota.value !== null &&
    Number.isSafeInteger(Number(delta.value))
)

watch(
  () => props.open,
  (open) => {
    if (!open) return
    mode.value = 'grant'
    amount.value = '1'
  },
  { immediate: true }
)

function close() {
  if (!saving.value) emit('close')
}

async function submit() {
  if (!valid.value || saving.value) return
  saving.value = true
  try {
    if (operation?.delta !== delta.value)
      operation = { delta: delta.value, id: crypto.randomUUID() }
    if (await props.save(delta.value, '', operation.id)) emit('close')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <ConsoleModal
    :open="open"
    :title="t('users.quotaTitle')"
    :subtitle="target ? target.username : ''"
    size="sm"
    @close="close"
  >
    <div class="sibling-space-y-5 text-left">
      <div
        class="sketch-md grid grid-cols-2 gap-1 bg-[var(--surface-muted)] p-1"
        role="radiogroup"
        :aria-label="t('users.quotaDirection')"
      >
        <button
          v-for="option in [
            { value: 'grant' as const, label: t('users.quotaGrant') },
            { value: 'deduct' as const, label: t('users.quotaDeduct') },
          ]"
          :key="option.value"
          type="button"
          role="radio"
          :aria-checked="mode === option.value"
          class="sketch-sm h-9 text-sm font-medium transition-colors focus-ring"
          :class="
            mode === option.value
              ? 'bg-[var(--surface-solid)] text-[var(--text-primary)] shadow-xs'
              : 'text-[var(--text-tertiary)] hover:text-[var(--text-secondary)]'
          "
          @click="mode = option.value"
        >
          {{ option.label }}
        </button>
      </div>

      <FormField :label="t('users.quotaAmount')" :hint="t('users.quotaHint')">
        <div class="relative">
          <span
            class="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-sm text-[var(--text-tertiary)]"
            aria-hidden="true"
            >$</span
          >
          <input
            v-model="amount"
            type="text"
            inputmode="decimal"
            name="admin-user-quota-amount"
            :aria-label="t('users.quotaAmount')"
            class="user-quota-number pl-8 focus-ring"
          />
        </div>
      </FormField>

      <dl
        class="sketch-md sibling-space-y-1.5 border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 text-xs"
      >
        <div class="flex items-center justify-between gap-3">
          <dt class="text-[var(--text-tertiary)]">
            {{ t('users.quotaCurrent') }}
          </dt>
          <dd
            class="break-all font-mono tabular-nums text-[var(--text-secondary)]"
          >
            {{ formatQuota(target?.quota ?? '0', 3) }}
          </dd>
        </div>
        <div class="flex items-center justify-between gap-3">
          <dt class="text-[var(--text-tertiary)]">
            {{ t('users.quotaNext') }}
          </dt>
          <dd
            class="break-all font-mono font-semibold tabular-nums"
            :style="
              nextQuota !== null && BigInt(nextQuota) < 0n
                ? 'color:var(--status-danger-text)'
                : 'color:var(--text-primary)'
            "
          >
            {{ nextQuota === null ? '—' : formatQuota(nextQuota, 3) }}
          </dd>
        </div>
      </dl>
    </div>

    <template #footer>
      <div class="grid grid-cols-2 gap-3">
        <ConsoleButton
          variant="secondary"
          size="lg"
          :disabled="saving"
          @click="close"
        >
          {{ t('common.cancel') }}
        </ConsoleButton>
        <ConsoleButton
          size="lg"
          :loading="saving"
          :disabled="!valid"
          @click="submit"
        >
          {{ t('users.quotaSave') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>

<style scoped>
.user-quota-number {
  width: 100%;
  height: 2.75rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  background: var(--surface-solid);
  padding: 0 1rem;
  color: var(--text-primary);
  font-size: 0.875rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  outline: none;
}

.user-quota-number:focus {
  border-color: var(--border-strong);
}
</style>
