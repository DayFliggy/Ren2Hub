<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import ActivityHero from '@/components/console/activity/ActivityHero.vue'
import ActivityCard from '@/components/console/activity/ActivityCard.vue'
import CheckinCard from '@/components/console/activity/CheckinCard.vue'
import TurnstileWidget from '@/components/auth/TurnstileWidget.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import SectionHeading from '@/components/common/SectionHeading.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatTileGrid from '@/components/common/StatTileGrid.vue'
import { useActivity } from '@/composables/useActivity'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { formatQuota } from '@/utils/format'
import { nativeAccountMessages } from '@/i18n/nativeAccount'
import { restoredConsoleMessages } from '@/i18n/restoredConsole'
import { activityRestorationMessages } from '@/i18n/activityRestoration'

const { t: rootT } = useI18n({ useScope: 'global' })
const { t } = useI18n({
  useScope: 'local',
  messages: {
    en: {
      ...nativeAccountMessages.en,
      ...restoredConsoleMessages.en,
      ...activityRestorationMessages.en,
    },
    'zh-CN': {
      ...nativeAccountMessages['zh-CN'],
      ...restoredConsoleMessages['zh-CN'],
      ...activityRestorationMessages['zh-CN'],
    },
  },
})
const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()
const { month, status, loading, claiming, loadError, load, checkin } =
  useActivity()
const turnstileToken = ref('')
const turnstileUnavailable = ref(false)
const turnstileWidget = ref<InstanceType<typeof TurnstileWidget> | null>(null)
const checkinDisabled = computed(
  () =>
    loading.value ||
    claiming.value ||
    Boolean(loadError.value) ||
    (app.turnstileEnabled &&
      (!app.turnstileSiteKey ||
        !turnstileToken.value ||
        turnstileUnavailable.value))
)
const inviteTiles = computed(() => [
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
    label: t('nativeAccount.history'),
    value:
      auth.user?.aff_history_quota === undefined
        ? '—'
        : formatQuota(auth.user.aff_history_quota),
  },
])
async function submit() {
  if (
    checkinDisabled.value ||
    !app.checkinEnabled ||
    !status.value?.enabled ||
    status.value.stats.checked_in_today
  )
    return
  await checkin(turnstileToken.value)
  turnstileToken.value = ''
  turnstileWidget.value?.reset()
}
watch([() => app.turnstileEnabled, () => app.turnstileSiteKey], () => {
  turnstileToken.value = ''
  turnstileUnavailable.value = false
})
watch(loading, (value) => {
  if (!value) return
  turnstileToken.value = ''
  turnstileUnavailable.value = false
})
</script>

<template>
  <div class="sibling-space-y-6">
    <ActivityHero
      :title="rootT('activity.title')"
      :subtitle="t('restored.activitySubtitle')"
      :pill="t('restored.activityPill')"
    />
    <section>
      <div class="mb-4 flex items-center justify-between">
        <SectionHeading>{{
          t('activityRestoration.dailyTitle')
        }}</SectionHeading>
        <button
          type="button"
          class="rounded-lg p-1.5 text-[var(--text-tertiary)] transition-colors hover:bg-[var(--state-hover-layer)] hover:text-[var(--text-secondary)] focus-ring"
          :disabled="loading || claiming"
          :aria-label="t('nativeAccount.refresh')"
          :title="t('nativeAccount.refresh')"
          @click="load"
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            :class="{ 'animate-spin': loading }"
            aria-hidden="true"
          >
            <path d="M21 12a9 9 0 1 1-9-9c2.52 0 4.93 1 6.74 2.74L21 8" />
            <path d="M21 3v5h-5" />
          </svg>
        </button>
      </div>
      <ErrorBanner v-if="loadError" :message="loadError" @retry="load" />
      <div class="grid gap-5 lg:grid-cols-2">
        <SkeletonBlock v-if="loading" bordered class="h-96 lg:col-span-2" />
        <article
          v-else-if="!app.checkinEnabled"
          class="pencil-surface rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-5 lg:col-span-2"
          data-handdrawn="surface"
        >
          <h3 class="text-base font-semibold text-[var(--text-primary)]">
            {{ t('nativeAccount.checkin') }}
          </h3>
          <p role="status" class="mt-4 text-sm text-[var(--text-tertiary)]">
            {{ t('nativeAccount.checkinDisabled') }}
          </p>
        </article>
        <CheckinCard
          v-else-if="status"
          v-model:month="month"
          class="lg:col-span-2"
          :status="status"
          :claiming="claiming"
          :disabled="checkinDisabled"
          @checkin="submit"
        >
          <TurnstileWidget
            v-if="
              app.turnstileEnabled &&
              status.enabled &&
              !status.stats.checked_in_today
            "
            ref="turnstileWidget"
            class="mt-4"
            :site-key="app.turnstileSiteKey"
            @verified="turnstileToken = $event"
            @unavailable="turnstileUnavailable = true"
          />
        </CheckinCard>
        <ActivityCard
          class="lg:col-span-2"
          :title="t('restored.inviteSummary')"
          :tagline="t('restored.shareHint')"
          :status-label="t('activityRestoration.accountTotals')"
          icon="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M16 3a4 4 0 0 1 0 8M22 21v-2a4 4 0 0 0-3-3.87M13 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z"
          :claim-label="t('restored.inviteOpen')"
          :footer-hint="t('activityRestoration.inviteHint')"
          @claim="router.push({ name: 'invite' })"
        >
          <StatTileGrid :tiles="inviteTiles" :columns="3" />
        </ActivityCard>
      </div>
    </section>
  </div>
</template>
