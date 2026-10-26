<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import CheckinCalendar from '@/components/console/activity/CheckinCalendar.vue'
import type { CheckinStatus } from '@/composables/useActivity'
import { activityRestorationMessages } from '@/i18n/activityRestoration'
import { nativeAccountMessages } from '@/i18n/nativeAccount'
import { formatQuota } from '@/utils/format'

const props = defineProps<{
  status: CheckinStatus
  month: string
  claiming: boolean
  disabled?: boolean
}>()
const emit = defineEmits<{ checkin: []; 'update:month': [value: string] }>()
const { t, locale } = useI18n({
  messages: {
    'zh-CN': {
      ...nativeAccountMessages['zh-CN'],
      ...activityRestorationMessages['zh-CN'],
    },
    en: {
      ...nativeAccountMessages.en,
      ...activityRestorationMessages.en,
    },
  },
})

const monthRecords = computed(() =>
  [...props.status.stats.records].sort((a, b) =>
    a.checkin_date.localeCompare(b.checkin_date)
  )
)
const monthQuota = computed(() =>
  monthRecords.value.reduce((sum, row) => sum + BigInt(row.quota_awarded), 0n)
)
// Show the server's date: the browser clock can sit in another time zone or
// pass midnight while the page stays open.
const today = computed(() =>
  new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeZone: 'UTC',
  }).format(new Date(`${props.status.today}T00:00:00Z`))
)
</script>

<template>
  <article
    class="pencil-surface overflow-hidden rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] lg:col-span-2"
    data-handdrawn="surface-clipped"
    data-checkin-card
  >
    <header
      class="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--border-subtle)] px-5 py-4"
    >
      <div class="flex min-w-0 flex-1 items-center gap-2.5">
        <span
          class="flex size-8 shrink-0 items-center justify-center rounded-lg"
          style="background: var(--accent-soft)"
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--accent-text)"
            stroke-width="1.8"
            aria-hidden="true"
          >
            <path
              d="M8 3v3m8-3v3M5 9h14M5 5h14a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2Z"
            />
          </svg>
        </span>
        <div class="min-w-0">
          <p class="text-sm font-bold tracking-wide text-[var(--text-primary)]">
            {{ t('nativeAccount.checkin') }}
          </p>
          <p class="text-[0.625rem] text-[var(--text-tertiary)]">
            {{ t('nativeAccount.checkinHint') }}
          </p>
        </div>
      </div>
      <span
        v-if="status.stats.checked_in_today"
        class="rounded-full bg-[var(--status-success-soft)] px-2.5 py-1 text-xs font-semibold text-[var(--status-success-text)]"
        >{{ t('nativeAccount.checked') }}</span
      >
      <span class="text-xs text-[var(--text-tertiary)]"
        >{{ t('activityRestoration.monthReward') }}
        <strong class="ml-1 text-[var(--accent-text)]">{{
          formatQuota(monthQuota.toString())
        }}</strong></span
      >
    </header>

    <div
      class="flex flex-col sm:flex-row sm:divide-x sm:divide-[var(--border-subtle)]"
    >
      <div
        class="relative flex flex-col items-center justify-center gap-3 px-6 py-6 sm:w-56 sm:shrink-0"
      >
        <p
          class="font-mono text-[0.6875rem] tracking-widest text-[var(--text-tertiary)]"
        >
          {{ today }}
        </p>
        <p
          class="text-4xl font-bold tracking-tighter text-[var(--text-primary)]"
        >
          {{ formatQuota(status.min_quota) }} –
          {{ formatQuota(status.max_quota) }}
        </p>
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t('nativeAccount.rewardRange') }}
        </p>
        <div
          class="checkin-stamp mt-1 rotate-[-14deg] rounded-md border-2 px-3 py-1 text-xs font-bold uppercase tracking-widest"
          :style="
            status.stats.checked_in_today
              ? 'border-color:var(--status-success);color:var(--status-success-text)'
              : 'border-color:var(--accent);color:var(--accent-text)'
          "
        >
          {{
            status.stats.checked_in_today
              ? t('nativeAccount.checked')
              : t('activityRestoration.notChecked')
          }}
        </div>
        <div
          class="checkin-perforation absolute bottom-0 left-5 right-5 h-px border-b border-dashed border-[var(--border-default)] sm:hidden"
        />
      </div>

      <div class="flex-1 px-5 py-5">
        <label
          class="mb-3 flex items-center justify-between gap-3 text-xs text-[var(--text-secondary)]"
        >
          {{ t('nativeAccount.month') }}
          <input
            :value="month"
            type="month"
            class="rounded-md border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-2 py-1"
            @input="
              emit('update:month', ($event.target as HTMLInputElement).value)
            "
          />
        </label>
        <CheckinCalendar :month="month" :records="status.stats.records" />
        <details class="mt-4 border-t border-[var(--border-subtle)] pt-3">
          <summary
            class="cursor-pointer text-xs font-semibold text-[var(--text-secondary)]"
          >
            {{ t('activityRestoration.records') }}
          </summary>
          <table
            v-if="monthRecords.length"
            class="mt-3 w-full text-left text-xs"
          >
            <caption class="mb-2 text-left text-[var(--text-tertiary)]">
              {{
                t('activityRestoration.records')
              }}
              ·
              {{
                month
              }}
            </caption>
            <thead>
              <tr>
                <th scope="col" class="py-2">{{ t('nativeAccount.date') }}</th>
                <th scope="col" class="py-2 text-right">
                  {{ t('nativeAccount.quota') }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="record in monthRecords" :key="record.checkin_date">
                <th scope="row" class="py-1 font-normal">
                  <time :datetime="record.checkin_date">{{
                    record.checkin_date
                  }}</time>
                </th>
                <td class="py-1 text-right text-[var(--accent-text)]">
                  {{ formatQuota(record.quota_awarded) }}
                </td>
              </tr>
            </tbody>
          </table>
          <p
            v-else
            class="py-5 text-center text-xs text-[var(--text-tertiary)]"
          >
            {{ t('activityRestoration.noRecords') }}
          </p>
        </details>
      </div>
    </div>

    <div class="border-t border-[var(--border-subtle)] px-5 py-4">
      <div class="flex flex-wrap items-center gap-x-5 gap-y-2">
        <div class="flex items-center gap-1.5">
          <span class="text-xs text-[var(--text-tertiary)]">{{
            t('nativeAccount.days')
          }}</span
          ><span class="text-sm font-bold text-[var(--text-primary)]">{{
            status.stats.total_checkins
          }}</span>
        </div>
        <div class="flex items-center gap-1.5">
          <span class="text-xs text-[var(--text-tertiary)]">{{
            t('nativeAccount.rewards')
          }}</span
          ><span class="text-sm font-bold text-[var(--accent-text)]">{{
            formatQuota(status.stats.total_quota)
          }}</span>
        </div>
        <ConsoleButton
          data-checkin-action
          size="sm"
          class="ml-auto"
          :loading="claiming"
          :disabled="disabled || status.stats.checked_in_today"
          @click="emit('checkin')"
          >{{
            status.stats.checked_in_today
              ? t('nativeAccount.checked')
              : t('nativeAccount.checkinNow')
          }}</ConsoleButton
        >
      </div>
    </div>
  </article>
</template>

<style scoped>
html.dark .checkin-stamp {
  rotate: initial;
  transform: none;
}

html.dark .checkin-perforation {
  border-bottom-style: solid;
}
</style>
