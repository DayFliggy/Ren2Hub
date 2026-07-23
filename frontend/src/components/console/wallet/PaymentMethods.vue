<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import ConsoleCard from '@/components/common/ConsoleCard.vue'

const emit = defineEmits<{
  select: [method: string]
}>()

const { t } = useI18n()

/** Bank-card stack pattern (design image 07), mapped to palette gradients. */
/** Each gradient pairs with a text color readable in both themes (see SubscriptionView). */
const methods = [
  {
    id: 'epay',
    name: () => t('wallet.epay'),
    desc: () => t('wallet.epayDesc'),
    last: 'EPAY',
    gradient:
      'linear-gradient(135deg, var(--accent) 0%, var(--accent-active) 100%)',
    fg: 'var(--accent-contrast)',
  },
  {
    id: 'stripe',
    name: () => t('wallet.stripe'),
    desc: () => t('wallet.stripeDesc'),
    last: 'STRP',
    gradient:
      'linear-gradient(135deg, var(--signal-strong) 0%, var(--signal-deep) 100%)',
    fg: 'var(--on-colored)',
  },
  {
    id: 'creem',
    name: () => t('wallet.creem'),
    desc: () => t('wallet.creemDesc'),
    last: 'CREM',
    gradient:
      'linear-gradient(135deg, var(--support) 0%, var(--support-strong) 100%)',
    fg: 'var(--accent-contrast)',
  },
]
</script>

<template>
  <ConsoleCard :title="t('wallet.payMethods', { count: methods.length })">
    <div class="space-y-3">
      <button
        v-for="m in methods"
        :key="m.id"
        type="button"
        class="block w-full rounded-xl p-4 text-left transition-transform hover:-translate-y-0.5 focus-ring"
        :style="{ background: m.gradient, color: m.fg }"
        @click="emit('select', m.id)"
      >
        <div class="flex items-center justify-between">
          <p class="text-sm font-bold">{{ m.name() }}</p>
          <span class="font-mono text-[10px] opacity-75"
            >•••• {{ m.last }}</span
          >
        </div>
        <p class="mt-6 text-xs opacity-80">{{ m.desc() }}</p>
      </button>

      <div
        class="rounded-xl border border-dashed border-[var(--border-default)] px-4 py-3 text-center text-sm text-[var(--text-tertiary)]"
      >
        {{ t('wallet.addMethod') }}
      </div>
    </div>
  </ConsoleCard>
</template>
