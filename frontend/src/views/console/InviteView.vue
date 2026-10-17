<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import PageHero from '@/components/console/PageHero.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import { useInvite } from '@/composables/useInvite'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import { formatQuota } from '@/utils/format'
import { nativeAccountMessages } from '@/i18n/nativeAccount'
const { t } = useI18n({ messages: nativeAccountMessages }),
  auth = useAuthStore(),
  toast = useToast()
const {
  code,
  inviteLink,
  loading,
  error,
  amount,
  transferring,
  canTransfer,
  load,
  transfer,
} = useInvite()
onMounted(load)
async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    toast.success(t('nativeAccount.done'))
  } catch (e) {
    toast.error(String(e))
  }
}
</script>
<template>
  <div>
    <PageHero
      :title="t('nativeAccount.invite')"
      :crumbs="[t('nativeAccount.invite')]"
    />
    <ErrorBanner v-if="error" :message="error" />
    <div class="grid gap-6 lg:grid-cols-2">
      <ConsoleCard :title="t('nativeAccount.invite')"
        ><p class="mb-4">{{ t('nativeAccount.inviteHint') }}</p>
        <p>
          {{ t('nativeAccount.invited') }}: {{ auth.user?.aff_count ?? '—' }}
        </p>
        <p class="mt-4">
          {{ t('nativeAccount.code') }}: <code>{{ code || '—' }}</code>
          <ConsoleButton :disabled="!code" @click="copy(code)">{{
            t('nativeAccount.copy')
          }}</ConsoleButton>
        </p>
        <label class="mt-4 block"
          >{{ t('nativeAccount.link')
          }}<input
            :value="code ? inviteLink : ''"
            readonly
            class="console-input mt-2 w-full" /></label
        ><ConsoleButton
          class="mt-2"
          :disabled="!code"
          @click="copy(inviteLink)"
          >{{ t('nativeAccount.copy') }}</ConsoleButton
        ></ConsoleCard
      >
      <ConsoleCard :title="t('nativeAccount.transfer')"
        ><p>
          {{ t('nativeAccount.available') }}:
          {{ formatQuota(auth.user?.aff_quota ?? 0) }}
        </p>
        <p>
          {{ t('nativeAccount.history') }}:
          {{ formatQuota(auth.user?.aff_history_quota ?? 0) }}
        </p>
        <label class="my-4 block"
          >{{ t('nativeAccount.amount')
          }}<input
            v-model="amount"
            inputmode="decimal"
            class="console-input mt-2 w-full"
        /></label>
        <p class="mb-4 text-sm">{{ t('nativeAccount.minimum') }}</p>
        <ConsoleButton
          :disabled="loading || transferring || !canTransfer"
          @click="transfer"
          >{{ t('nativeAccount.transfer') }}</ConsoleButton
        ></ConsoleCard
      >
    </div>
  </div>
</template>
