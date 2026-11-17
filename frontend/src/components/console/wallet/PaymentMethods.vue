<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import PaymentMethodIcon from './PaymentMethodIcon.vue'
import { choiceClass } from './choiceStyle'

const props = defineProps<{
  modelValue: string
  methods: Array<{ value: string; label: string; icon: string }>
}>()

const emit = defineEmits<{
  'update:modelValue': [method: string]
}>()

const { t } = useI18n()
</script>

<template>
  <div
    v-if="methods.length"
    role="group"
    :aria-label="t('wallet.payMethodLabel')"
    class="grid grid-cols-2 gap-2"
  >
    <button
      v-for="method in methods"
      :key="method.value"
      type="button"
      class="flex h-11 min-w-0 items-center justify-center gap-2 rounded-xl border px-3 text-sm font-semibold transition-colors focus-ring"
      :class="choiceClass(props.modelValue === method.value)"
      :aria-pressed="props.modelValue === method.value"
      @click="emit('update:modelValue', method.value)"
    >
      <PaymentMethodIcon :icon="method.icon" :type="method.value" />
      <span class="min-w-0 truncate">{{ method.label }}</span>
    </button>
  </div>
</template>
