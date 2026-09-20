<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import { useAuthStore } from '@/stores/auth'
import { formatQuota } from '@/utils/format'
import { MAX_QUOTA } from '@/utils/quota'

const props = defineProps<{
  transferable: string
  frozen: string
  rewardTotal: string
  invited: number
  readonly?: boolean
}>()

const emit = defineEmits<{ transfer: [] }>()

const { t } = useI18n()
const auth = useAuthStore()

const currentBalance = computed(() => auth.user?.available_quota ?? 0)
const afterBalance = computed(() => {
  const total = BigInt(currentBalance.value) + BigInt(props.transferable)
  return total <= MAX_QUOTA ? total : null
})
const canTransfer = computed(
  () =>
    !props.readonly &&
    BigInt(props.transferable) >= 500000n &&
    MAX_QUOTA - BigInt(currentBalance.value) >= 500000n
)
</script>

<template>
  <ConsoleCard :title="t('invite.rewardBalanceTitle')">
    <p class="text-xs leading-relaxed text-[var(--text-tertiary)]">
      {{ t('invite.rewardBalanceSubtitle', { count: invited }) }}
    </p>

    <p class="mt-4 text-3xl font-bold text-[var(--accent-text)]">
      {{ formatQuota(transferable) }}
    </p>

    <dl class="mt-4 space-y-2.5 text-sm">
      <div class="flex items-center justify-between">
        <dt class="text-[var(--text-tertiary)]">
          {{ t('invite.currentBalance') }}
        </dt>
        <dd class="font-medium text-[var(--text-primary)]">
          {{ formatQuota(currentBalance) }}
        </dd>
      </div>
      <div class="flex items-center justify-between">
        <dt class="text-[var(--text-tertiary)]">
          {{ t('invite.frozenReward') }}
        </dt>
        <dd class="font-medium text-[var(--text-primary)]">
          {{ formatQuota(frozen) }}
        </dd>
      </div>
      <div
        class="flex items-center justify-between border-t border-[var(--border-subtle)] pt-2.5"
      >
        <dt class="text-[var(--text-secondary)]">
          {{ t('invite.balanceAfterTransfer') }}
        </dt>
        <dd class="font-semibold text-[var(--text-primary)]">
          {{ afterBalance === null ? '—' : formatQuota(afterBalance) }}
        </dd>
      </div>
    </dl>

    <ConsoleButton
      class="mt-5"
      block
      :variant="canTransfer ? 'primary' : 'secondary'"
      :disabled="!canTransfer"
      @click="emit('transfer')"
    >
      {{ canTransfer ? t('invite.transfer') : t('invite.minimumTransfer') }}
    </ConsoleButton>

    <p
      class="mt-4 flex items-center justify-between border-t border-[var(--border-subtle)] pt-4 text-xs text-[var(--text-tertiary)]"
    >
      <span>{{ t('invite.rewardHistoryTotal') }}</span>
      <span class="font-medium text-[var(--text-secondary)]">
        {{ formatQuota(rewardTotal) }}
      </span>
    </p>
  </ConsoleCard>
</template>
