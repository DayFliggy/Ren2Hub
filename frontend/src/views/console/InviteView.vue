<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import PageHero from '@/components/console/PageHero.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import UnavailablePanel from '@/components/console/dashboard/UnavailablePanel.vue'
import InviteAffiliateCard from '@/components/console/invite/InviteAffiliateCard.vue'
import { useInvite } from '@/composables/useInvite'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import { formatQuota } from '@/utils/format'
import { nativeAccountMessages } from '@/i18n/nativeAccount'
import { restoredConsoleMessages } from '@/i18n/restoredConsole'
const { t } = useI18n({
  useScope: 'local',
  messages: {
    en: { ...nativeAccountMessages.en, ...restoredConsoleMessages.en },
    'zh-CN': {
      ...nativeAccountMessages['zh-CN'],
      ...restoredConsoleMessages['zh-CN'],
    },
  },
})
const auth = useAuthStore(),
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
const transferOpen = ref(false)
const stats = computed(() => [
  {
    label: t('nativeAccount.invited'),
    value: String(auth.user?.aff_count ?? '—'),
  },
  {
    label: t('nativeAccount.available'),
    value:
      auth.user?.aff_quota === undefined
        ? '—'
        : formatQuota(auth.user.aff_quota),
  },
  {
    label: t('restored.rebate'),
    value: '—',
    hint: t('restored.referralUnavailable'),
  },
  {
    label: t('restored.frozen'),
    value: '—',
    hint: t('restored.referralUnavailable'),
  },
])
onMounted(load)
async function copy(value: string) {
  if (!code.value) return
  try {
    await navigator.clipboard.writeText(value)
    toast.success(t('nativeAccount.done'))
  } catch (e) {
    toast.error(String(e))
  }
}
function share(target: 'x' | 'telegram' | 'email') {
  if (!code.value) return
  const link = encodeURIComponent(inviteLink.value)
  const url =
    target === 'x'
      ? `https://twitter.com/intent/tweet?url=${link}`
      : target === 'telegram'
        ? `https://t.me/share/url?url=${link}`
        : `mailto:?body=${link}`
  window.open(url, '_blank', 'noopener,noreferrer')
}
async function submitTransfer() {
  await transfer()
  if (!error.value) transferOpen.value = false
}
</script>
<template>
  <div class="sibling-space-y-6">
    <PageHero
      :title="t('nativeAccount.invite')"
      :crumbs="[t('nativeAccount.invite')]"
      ><template #actions
        ><div class="text-right">
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ t('nativeAccount.history') }}
          </p>
          <p class="display-number mt-1 text-3xl">
            {{
              auth.user?.aff_history_quota === undefined
                ? '—'
                : formatQuota(auth.user.aff_history_quota)
            }}
          </p>
        </div></template
      ></PageHero
    >
    <ErrorBanner v-if="error" :message="error" @retry="load" />
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <ConsoleCard v-for="stat in stats" :key="stat.label"
        ><p class="text-xs text-[var(--text-tertiary)]">{{ stat.label }}</p>
        <p class="display-number mt-2 text-2xl">{{ stat.value }}</p>
        <p
          v-if="stat.hint"
          class="mt-2 text-xs leading-5 text-[var(--text-tertiary)]"
        >
          {{ stat.hint }}
        </p></ConsoleCard
      >
    </div>
    <div class="grid gap-5 lg:grid-cols-[minmax(0,1fr)_22.5rem]">
      <InviteAffiliateCard
        :code="code"
        :invite-link="code ? inviteLink : ''"
        @copy-code="copy(code)"
        @copy-link="copy(inviteLink)"
        @share-x="share('x')"
        @share-telegram="share('telegram')"
        @share-email="share('email')"
      />
      <UnavailablePanel
        :title="t('restored.inviteHistory')"
        :hint="t('restored.inviteHistoryHint')"
      />
      <UnavailablePanel
        :title="t('restored.inviteTrend')"
        :hint="t('restored.inviteTrendHint')"
      />
      <ConsoleCard
        :title="t('nativeAccount.transfer')"
        variant="ink"
        data-affiliate-transfer
        ><p class="text-sm">{{ t('nativeAccount.available') }}</p>
        <p class="display-number mt-3 text-3xl">
          {{
            auth.user?.aff_quota === undefined
              ? '—'
              : formatQuota(auth.user.aff_quota)
          }}
        </p>
        <p class="my-4 text-sm leading-6">{{ t('nativeAccount.minimum') }}</p>
        <ConsoleButton
          :disabled="
            loading || transferring || (auth.user?.aff_quota ?? 0) <= 0
          "
          @click="transferOpen = true"
          >{{ t('nativeAccount.transfer') }}</ConsoleButton
        ></ConsoleCard
      >
    </div>
    <section>
      <h2 class="mb-4 text-lg font-semibold">{{ t('restored.howTitle') }}</h2>
      <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <ConsoleCard v-for="n in 4" :key="n"
          ><span class="display-number text-2xl text-[var(--accent-text)]"
            >0{{ n }}</span
          >
          <h3 class="mt-3 text-sm font-semibold">
            {{ t(`restored.how${n}`) }}
          </h3>
          <p class="mt-2 text-xs leading-6 text-[var(--text-tertiary)]">
            {{ t(`restored.how${n}Hint`) }}
          </p></ConsoleCard
        >
      </div>
    </section>
    <ConsoleModal
      :open="transferOpen"
      :title="t('nativeAccount.transfer')"
      size="sm"
      @close="transferOpen = false"
      ><ErrorBanner v-if="error" :message="error" /><label class="block text-sm"
        >{{ t('nativeAccount.amount')
        }}<input
          v-model="amount"
          inputmode="decimal"
          class="console-input mt-2 w-full"
          :disabled="transferring"
      /></label>
      <p class="my-4 text-sm">{{ t('nativeAccount.minimum') }}</p>
      <template #footer
        ><ConsoleButton
          :disabled="loading || transferring || !canTransfer"
          @click="submitTransfer"
          >{{ t('nativeAccount.transfer') }}</ConsoleButton
        ></template
      ></ConsoleModal
    >
  </div>
</template>
