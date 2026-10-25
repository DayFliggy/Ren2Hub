<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import CheckinCalendar from './CheckinCalendar.vue'
import type { CheckinStatus } from '@/composables/useActivity'
import { activityRestorationMessages } from '@/i18n/activityRestoration'
import { nativeAccountMessages } from '@/i18n/nativeAccount'
import { formatQuota } from '@/utils/format'

const props = defineProps<{
  status: CheckinStatus
  month: string
  claiming: boolean
  disabled: boolean
}>()
const emit = defineEmits<{
  checkin: []
  'update:month': [month: string]
}>()
const { t } = useI18n({
  useScope: 'local',
  messages: {
    en: { ...nativeAccountMessages.en, ...activityRestorationMessages.en },
    'zh-CN': {
      ...nativeAccountMessages['zh-CN'],
      ...activityRestorationMessages['zh-CN'],
    },
  },
})
const monthReward = computed(() =>
  props.status.stats.records.reduce(
    (sum, record) => sum + record.quota_awarded,
    0
  )
)
const monthDays = computed(() => {
  if (!/^\d{4}-\d{2}$/.test(props.month)) return 0
  const [year, month] = props.month.split('-').map(Number) as [number, number]
  return month >= 1 && month <= 12
    ? new Date(Date.UTC(year, month, 0)).getUTCDate()
    : 0
})
const monthPct = computed(() =>
  monthDays.value
    ? Math.min(
        100,
        Math.round((props.status.stats.checkin_count / monthDays.value) * 100)
      )
    : 0
)
</script>

<template>
  <article
    class="pencil-surface overflow-hidden rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
    data-handdrawn="surface-clipped"
    data-checkin-card
  >
    <div
      class="flex flex-wrap items-start justify-between gap-3 border-b border-[var(--border-subtle)] px-5 py-4"
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
            <rect x="3" y="5" width="18" height="16" rx="2" />
            <path d="M16 3v4M8 3v4M3 11h18m-11 5 2 2 4-4" />
          </svg>
        </span>
        <div class="min-w-0">
          <h3
            class="text-sm font-bold tracking-wide text-[var(--text-primary)]"
          >
            {{ t('nativeAccount.checkin') }}
          </h3>
          <p class="text-[0.625rem] text-[var(--text-tertiary)]">
            {{ t('activityRestoration.randomReward') }}
          </p>
        </div>
      </div>
      <span class="text-xs text-[var(--text-tertiary)]">
        {{ t('activityRestoration.monthReward') }}
        <span class="ml-1 font-semibold text-[var(--accent-text)]">{{
          formatQuota(monthReward)
        }}</span>
      </span>
    </div>

    <div
      class="flex flex-col xl:flex-row xl:sibling-divide-x xl:sibling-divide-[var(--border-subtle)]"
    >
      <div
        class="relative flex flex-col items-center justify-center gap-3 px-6 py-6 xl:w-56 xl:shrink-0"
      >
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t('nativeAccount.rewardRange') }}
        </p>
        <p
          class="text-center text-2xl font-bold tracking-tighter text-[var(--text-primary)]"
        >
          <span class="inline-block max-w-full break-all">{{
            formatQuota(status.min_quota)
          }}</span>
          –
          <span class="inline-block max-w-full break-all">{{
            formatQuota(status.max_quota)
          }}</span>
        </p>
        <p class="text-center text-xs leading-5 text-[var(--text-tertiary)]">
          {{ t('nativeAccount.checkinHint') }}
        </p>
        <div
          class="checkin-stamp mt-1 rotate-[-14deg] rounded-md border-2 px-3 py-1 text-xs font-bold uppercase tracking-widest"
          :style="
            status.stats.checked_in_today
              ? 'border-color:var(--status-danger);color:var(--status-danger-text)'
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
          class="checkin-perforation absolute bottom-0 left-5 right-5 h-px border-b border-dashed border-[var(--border-default)] xl:hidden"
        />
      </div>

      <div class="min-w-0 flex-1 px-5 py-5">
        <label
          class="mb-3 flex flex-wrap items-center justify-between gap-2 text-xs font-semibold text-[var(--text-secondary)]"
        >
          {{ t('nativeAccount.month') }}
          <input
            :value="month"
            type="month"
            class="console-input min-w-0 max-w-full text-xs"
            @input="
              emit('update:month', ($event.target as HTMLInputElement).value)
            "
          />
        </label>
        <CheckinCalendar :month="month" :records="status.stats.records" />
        <details class="mt-4 text-sm">
          <summary
            class="cursor-pointer rounded-lg text-xs font-semibold text-[var(--text-secondary)] focus-ring"
          >
            {{ t('activityRestoration.records') }}
          </summary>
          <table
            v-if="status.stats.records.length"
            class="mt-3 w-full table-fixed border-separate border-spacing-y-1 text-left text-xs"
          >
            <caption class="sr-only">
              {{
                t('activityRestoration.records')
              }}
              ·
              {{
                month
              }}
            </caption>
            <thead>
              <tr class="text-[var(--text-tertiary)]">
                <th scope="col" class="px-2 py-1 font-medium">
                  {{ t('nativeAccount.date') }}
                </th>
                <th scope="col" class="px-2 py-1 text-right font-medium">
                  {{ t('nativeAccount.quota') }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="record in status.stats.records"
                :key="record.checkin_date"
                class="transition-colors hover:bg-[var(--surface-muted)]"
              >
                <th
                  scope="row"
                  class="rounded-l-lg px-2 py-1.5 font-mono font-normal text-[var(--text-secondary)]"
                >
                  <time :datetime="record.checkin_date">{{
                    record.checkin_date
                  }}</time>
                </th>
                <td
                  class="rounded-r-lg break-all px-2 py-1.5 text-right font-semibold tabular-nums text-[var(--accent-text)]"
                >
                  {{ formatQuota(record.quota_awarded) }}
                </td>
              </tr>
            </tbody>
          </table>
          <p
            v-if="!status.stats.records.length"
            class="mt-2 text-[var(--text-tertiary)]"
          >
            {{ t('activityRestoration.noRecords') }}
          </p>
        </details>
      </div>
    </div>

    <div class="border-t border-[var(--border-subtle)] px-5 py-4">
      <div class="mb-3">
        <div
          class="mb-1.5 flex items-center justify-between text-[0.625rem] text-[var(--text-tertiary)]"
        >
          <span>{{ t('activityRestoration.monthProgress') }}</span>
          <span class="font-medium"
            >{{ status.stats.checkin_count }} / {{ monthDays }}</span
          >
        </div>
        <div
          class="pencil-progress h-1.5 overflow-hidden rounded-full bg-[var(--surface-muted)]"
        >
          <div
            class="h-full rounded-full transition-all duration-700"
            style="background: var(--accent)"
            :style="{ width: `${monthPct}%` }"
          />
        </div>
      </div>
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
          size="sm"
          :loading="claiming"
          :disabled="
            disabled || !status.enabled || status.stats.checked_in_today
          "
          class="ml-auto"
          data-checkin-action
          @click="emit('checkin')"
          >{{
            status.stats.checked_in_today
              ? t('nativeAccount.checked')
              : t('nativeAccount.checkinNow')
          }}</ConsoleButton
        >
      </div>
      <slot />
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
