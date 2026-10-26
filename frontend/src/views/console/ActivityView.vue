<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import TurnstileWidget from '@/components/auth/TurnstileWidget.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import SectionHeading from '@/components/common/SectionHeading.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import ActivityHero from '@/components/console/activity/ActivityHero.vue'
import CheckinCard from '@/components/console/activity/CheckinCard.vue'
import { useActivity } from '@/composables/useActivity'
import { nativeAccountMessages } from '@/i18n/nativeAccount'
import { restoredConsoleMessages } from '@/i18n/restoredConsole'
import { activityRestorationMessages } from '@/i18n/activityRestoration'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import { formatQuota } from '@/utils/format'

const { t: accountT } = useI18n({
  messages: {
    'zh-CN': {
      ...nativeAccountMessages['zh-CN'],
      ...restoredConsoleMessages['zh-CN'],
      ...activityRestorationMessages['zh-CN'],
    },
    en: {
      ...nativeAccountMessages.en,
      ...restoredConsoleMessages.en,
      ...activityRestorationMessages.en,
    },
  },
})
const { t } = useI18n()
const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()
const toast = useToast()
const { month, status, loading, loadError, claiming, load, checkin } =
  useActivity()
const turnstileToken = ref('')
const turnstileUnavailable = ref(false)
const turnstileWidget = ref<InstanceType<typeof TurnstileWidget> | null>(null)

const checkinEnabled = computed(
  () => app.checkinEnabled && status.value?.enabled === true
)
const turnstileReady = computed(
  () =>
    !app.turnstileEnabled ||
    Boolean(
      app.turnstileSiteKey &&
      turnstileToken.value &&
      !turnstileUnavailable.value
    )
)

async function handleCheckin() {
  if (
    loading.value ||
    loadError.value ||
    !checkinEnabled.value ||
    status.value?.stats.checked_in_today
  )
    return
  if (!app.turnstileEnabled) {
    await checkin()
    return
  }
  if (!app.turnstileSiteKey || turnstileUnavailable.value) {
    toast.error(t('common.turnstileUnavailable'))
    return
  }
  if (!turnstileToken.value) {
    toast.error(t('common.turnstileRequired'))
    return
  }
  const token = turnstileToken.value
  turnstileToken.value = ''
  await checkin(token)
  turnstileWidget.value?.reset()
}

function handleTurnstileVerified(token: string) {
  turnstileToken.value = token
  turnstileUnavailable.value = false
}

// A reload or config change remounts the widget, so the previous challenge state is stale.
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
      :title="t('activity.title')"
      :subtitle="accountT('restored.activitySubtitle')"
      :pill="accountT('restored.activityPill')"
    />

    <section>
      <div class="mb-4 flex items-center justify-between">
        <SectionHeading>{{ t('activity.dailyTasks') }}</SectionHeading>
        <button
          type="button"
          class="rounded-lg p-1.5 text-[var(--text-tertiary)] hover:bg-[var(--state-hover-layer)] focus-ring"
          :disabled="loading"
          :aria-label="accountT('nativeAccount.refresh')"
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
      <!-- The invite totals come from the account, so a failed check-in load must not hide them. -->
      <div class="grid gap-5 lg:grid-cols-2">
        <SkeletonBlock v-if="loading" bordered class="h-64 lg:col-span-2" />
        <ErrorBanner
          v-else-if="loadError"
          class="lg:col-span-2"
          :message="loadError"
          @retry="load()"
        />
        <template v-else>
          <TurnstileWidget
            v-if="
              app.turnstileEnabled &&
              status?.enabled &&
              !status.stats.checked_in_today
            "
            ref="turnstileWidget"
            class="lg:col-span-2"
            :site-key="app.turnstileSiteKey"
            @verified="handleTurnstileVerified"
            @unavailable="turnstileUnavailable = true"
          />
          <CheckinCard
            v-if="status"
            class="lg:col-span-2"
            :status="status"
            :month="month"
            :claiming="claiming"
            :disabled="!checkinEnabled || !turnstileReady"
            @update:month="month = $event"
            @checkin="handleCheckin"
          />
          <div
            v-else
            role="status"
            class="lg:col-span-2 rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-5 text-sm text-[var(--text-secondary)]"
          >
            {{ accountT('nativeAccount.checkinDisabled') }}
          </div>
        </template>
        <section
          class="pencil-surface lg:col-span-2 rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-5"
          data-handdrawn="surface"
        >
          <p
            class="text-xs font-semibold uppercase tracking-wide text-[var(--accent-text)]"
          >
            {{ t('activity.invite.tagline') }}
          </p>
          <h3 class="mt-2 text-lg font-semibold text-[var(--text-primary)]">
            {{ t('activity.invite.title') }}
          </h3>
          <p class="mt-2 text-sm leading-relaxed text-[var(--text-tertiary)]">
            {{ accountT('nativeAccount.inviteHint') }}
          </p>
          <p class="mt-4 text-xs font-semibold text-[var(--text-tertiary)]">
            {{ accountT('activityRestoration.accountTotals') }}
          </p>
          <dl class="mt-3 grid gap-4 sm:grid-cols-3">
            <div>
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ accountT('nativeAccount.invited') }}
              </dt>
              <dd class="mt-1 text-xl font-semibold text-[var(--text-primary)]">
                {{ auth.user?.aff_count ?? '—' }}
              </dd>
            </div>
            <div>
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ accountT('nativeAccount.available') }}
              </dt>
              <dd class="mt-1 text-xl font-semibold text-[var(--accent-text)]">
                {{
                  auth.user?.aff_quota == null
                    ? '—'
                    : formatQuota(auth.user.aff_quota)
                }}
              </dd>
            </div>
            <div>
              <dt class="text-xs text-[var(--text-tertiary)]">
                {{ accountT('nativeAccount.history') }}
              </dt>
              <dd class="mt-1 text-xl font-semibold text-[var(--text-primary)]">
                {{
                  auth.user?.aff_history_quota == null
                    ? '—'
                    : formatQuota(auth.user.aff_history_quota)
                }}
              </dd>
            </div>
          </dl>
          <button
            type="button"
            class="mt-4 text-sm font-semibold text-[var(--accent-text)] hover:underline focus-ring"
            @click="router.push({ name: 'invite' })"
          >
            {{ accountT('restored.inviteOpen') }}
          </button>
        </section>
      </div>
    </section>
  </div>
</template>
