<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Gamepad2, Wheat } from 'lucide-vue-next'
import ActivityHero from '@/components/console/activity/ActivityHero.vue'
import CheckinCalendar from '@/components/console/activity/CheckinCalendar.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import StatTileGrid from '@/components/common/StatTileGrid.vue'
import UnavailablePanel from '@/components/console/dashboard/UnavailablePanel.vue'
import ActivityEntryCard from '@/components/console/ActivityEntryCard.vue'
import TurnstileWidget from '@/components/auth/TurnstileWidget.vue'
import { useActivity } from '@/composables/useActivity'
import { useThemedAsset } from '@/composables/useThemedAsset'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { formatQuota } from '@/utils/format'
import { nativeAccountMessages } from '@/i18n/nativeAccount'
import { restoredConsoleMessages } from '@/i18n/restoredConsole'
import farmDay from '@/assets/activity/farm-banner-day-sketch.webp'
import farmNight from '@/assets/activity/farm-banner.webp'
import gameDay from '@/assets/activity/bigame-banner-day-sketch.webp'
import gameNight from '@/assets/activity/bigame-banner.webp'
const { t: rootT } = useI18n({ useScope: 'global' })
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
const app = useAppStore(),
  auth = useAuthStore()
const { month, status, loading, claiming, loadError, load, checkin } =
  useActivity()
const turnstile = ref(''),
  widget = ref<InstanceType<typeof TurnstileWidget> | null>(null)
const farmBanner = useThemedAsset(farmDay, farmNight),
  gameBanner = useThemedAsset(gameDay, gameNight)
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
  await checkin(turnstile.value)
  turnstile.value = ''
  widget.value?.reset()
}
</script>
<template>
  <div class="sibling-space-y-6">
    <ActivityHero
      :title="rootT('activity.title')"
      :subtitle="t('restored.activitySubtitle')"
      :pill="t('restored.activityPill')"
    />
    <section>
      <h2 class="mb-3 text-lg font-semibold">{{ t('restored.campaigns') }}</h2>
      <div class="grid gap-4 md:grid-cols-2">
        <ActivityEntryCard
          :title="rootT('nav.farm')"
          :subtitle="t('restored.campaignsHint')"
          :tag="t('restored.unavailable')"
          :image="farmBanner"
          gradient="linear-gradient(120deg, var(--signal-strong), var(--accent))"
          :icon="Wheat"
          :cta="t('restored.unavailable')"
          disabled
        />
        <ActivityEntryCard
          :title="rootT('nav.bigame')"
          :subtitle="t('restored.campaignsHint')"
          :tag="t('restored.unavailable')"
          :image="gameBanner"
          gradient="linear-gradient(120deg, var(--signal-deep), var(--accent))"
          :icon="Gamepad2"
          :cta="t('restored.unavailable')"
          disabled
        />
      </div>
    </section>
    <ErrorBanner v-if="loadError" :message="loadError" @retry="load" />
    <div class="grid gap-5 lg:grid-cols-2">
      <ConsoleCard :title="t('nativeAccount.checkin')">
        <p v-if="!app.checkinEnabled" role="status">
          {{ t('nativeAccount.checkinDisabled') }}
        </p>
        <template v-else>
          <p class="mb-4 text-sm leading-6 text-[var(--text-tertiary)]">
            {{ t('nativeAccount.checkinHint') }}
          </p>
          <div v-if="status" class="sibling-space-y-4">
            <p>
              {{ t('nativeAccount.rewardRange') }}:
              {{ formatQuota(status.min_quota) }} –
              {{ formatQuota(status.max_quota) }}
            </p>
            <StatTileGrid
              :columns="2"
              :tiles="[
                {
                  label: t('nativeAccount.days'),
                  value: String(status.stats.total_checkins),
                },
                {
                  label: t('nativeAccount.rewards'),
                  value: formatQuota(status.stats.total_quota),
                },
              ]"
            />
            <TurnstileWidget
              v-if="app.turnstileEnabled"
              ref="widget"
              :site-key="app.turnstileSiteKey"
              @verified="turnstile = $event"
            />
            <ConsoleButton
              data-checkin-action
              :disabled="
                !status.enabled ||
                loading ||
                claiming ||
                status.stats.checked_in_today ||
                (app.turnstileEnabled && !turnstile)
              "
              @click="submit"
              >{{
                status.stats.checked_in_today
                  ? t('nativeAccount.checked')
                  : t('nativeAccount.checkinNow')
              }}</ConsoleButton
            >
          </div>
          <p v-else-if="loading" class="text-sm">
            {{ rootT('common.loading') }}
          </p>
          <label class="my-4 block text-sm"
            >{{ t('nativeAccount.month')
            }}<input
              v-model="month"
              type="month"
              class="console-input mt-2 block"
          /></label>
          <CheckinCalendar
            v-if="status"
            :month="month"
            :records="status.stats.records"
          />
          <details v-if="status" class="mt-4 text-sm">
            <summary class="cursor-pointer">
              {{ t('restored.checkinRecords') }}
            </summary>
            <table class="mt-3 w-full text-left">
              <thead>
                <tr>
                  <th>{{ t('nativeAccount.date') }}</th>
                  <th>{{ t('nativeAccount.quota') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="record in status.stats.records"
                  :key="record.checkin_date"
                >
                  <td>{{ record.checkin_date }}</td>
                  <td>{{ formatQuota(record.quota_awarded) }}</td>
                </tr>
              </tbody>
            </table>
            <p
              v-if="!status.stats.records.length"
              class="mt-2 text-[var(--text-tertiary)]"
            >
              {{ t('restored.noCheckins') }}
            </p>
          </details>
        </template>
      </ConsoleCard>
      <UnavailablePanel
        :title="t('restored.newcomer')"
        :hint="t('restored.newcomerHint')"
      />
      <ConsoleCard class="lg:col-span-2" :title="t('restored.inviteSummary')"
        ><StatTileGrid :tiles="inviteTiles" :columns="3" /><RouterLink
          to="/invite"
          class="mt-5 inline-flex text-sm text-[var(--accent-text)] focus-ring"
          >{{ t('restored.inviteOpen') }}</RouterLink
        ></ConsoleCard
      >
    </div>
  </div>
</template>
