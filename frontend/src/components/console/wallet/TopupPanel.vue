<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import AmountInput from '@/components/common/AmountInput.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useToast } from '@/composables/useToast'

const props = withDefaults(
  defineProps<{
    activePanel?: 'topup' | 'redeem'
    paymentMethod?: string
  }>(),
  { activePanel: 'topup', paymentMethod: 'epay' }
)

const emit = defineEmits<{
  done: []
  'update:paymentMethod': [method: string]
}>()

const { t } = useI18n()
const toast = useToast()

const presets = [5, 10, 20, 50, 100]
const amount = ref<number | null>(20)
const method = computed({
  get: () => props.paymentMethod,
  set: (value: string) => emit('update:paymentMethod', value),
})
const code = ref('')
const submitting = ref(false)

const methodOptions = computed(() => [
  { value: 'epay', label: t('wallet.epay') },
  { value: 'stripe', label: t('wallet.stripe') },
  { value: 'creem', label: t('wallet.creem') },
])

watch(
  () => props.activePanel,
  (panel) => {
    if (panel === 'redeem') {
      nextTick(() => document.getElementById('redeem-input')?.focus())
    }
  },
  { immediate: true }
)

async function topup() {
  if (!amount.value) return
  submitting.value = true
  try {
    const res = await api.post<{ message: string }>('/api/user/topup', {
      amount: amount.value,
      method: method.value,
    })
    toast.success(res.message || t('wallet.callbackNote'))
    emit('done')
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    submitting.value = false
  }
}

async function redeem() {
  if (!code.value.trim()) return
  submitting.value = true
  try {
    const res = await api.post<{ message: string }>('/api/user/topup/redeem', {
      code: code.value.trim(),
    })
    toast.success(res.message)
    code.value = ''
    emit('done')
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <ConsoleCard :title="t('wallet.quickTopup')">
    <!-- preset amounts -->
    <div class="flex flex-wrap gap-2">
      <button
        v-for="preset in presets"
        :key="preset"
        type="button"
        class="h-10 rounded-xl px-4 text-sm font-semibold transition-all focus-ring"
        :style="
          amount === preset
            ? 'background:var(--accent);color:var(--accent-contrast)'
            : 'background:var(--surface-muted);color:var(--text-secondary)'
        "
        :class="{ 'hover:bg-[var(--surface-hover)]': amount !== preset }"
        @click="amount = preset"
      >
        ${{ preset }}
      </button>
    </div>

    <div class="mt-4 grid gap-3 sm:grid-cols-[1fr_150px_auto]">
      <FormField>
        <AmountInput
          v-model="amount"
          :placeholder="t('wallet.amountPlaceholder')"
          :min="1"
        />
      </FormField>
      <FilterSelect
        v-model="method"
        :options="methodOptions"
        :label="t('wallet.payMethodLabel')"
        class="h-11"
      />
      <ConsoleButton
        size="lg"
        :loading="submitting"
        :disabled="!amount"
        @click="topup"
      >
        {{ t('wallet.topupNow') }}
      </ConsoleButton>
    </div>

    <p
      class="mt-3 flex items-center gap-1.5 text-xs text-[var(--text-tertiary)]"
    >
      <span
        class="inline-block h-3.5 w-3.5 rounded-full border border-[var(--border-default)] text-center text-[10px] leading-3"
        >i</span
      >
      {{ t('wallet.callbackNote') }}
    </p>

    <!-- redeem -->
    <div class="mt-5 border-t border-[var(--border-subtle)] pt-4">
      <p class="mb-2.5 text-sm font-medium text-[var(--text-secondary)]">
        {{ t('wallet.redeemTitle') }}
      </p>
      <div class="grid gap-3 sm:grid-cols-[1fr_auto]">
        <TextInput
          id="redeem-input"
          v-model="code"
          :placeholder="t('wallet.redeemPlaceholder')"
        />
        <ConsoleButton
          variant="secondary"
          :loading="submitting"
          :disabled="!code.trim()"
          @click="redeem"
        >
          {{ t('wallet.redeemNow') }}
        </ConsoleButton>
      </div>
    </div>
  </ConsoleCard>
</template>
