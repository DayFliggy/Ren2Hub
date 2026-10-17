<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatQuota } from '@/utils/format'
import { restoredConsoleMessages } from '@/i18n/restoredConsole'
const props = defineProps<{
  month: string
  records: Array<{ checkin_date: string; quota_awarded: number }>
}>()
const { t, locale } = useI18n({ messages: restoredConsoleMessages })
const days = computed(() => {
  if (!/^\d{4}-\d{2}$/.test(props.month)) return []
  const [year, month] = props.month.split('-').map(Number) as [number, number]
  if (month < 1 || month > 12) return []
  const count = new Date(Date.UTC(year, month, 0)).getUTCDate()
  const offset = (new Date(Date.UTC(year, month - 1, 1)).getUTCDay() + 6) % 7
  const rewards = new Map(
    props.records.map((record) => [record.checkin_date, record.quota_awarded])
  )
  return [
    ...Array.from({ length: offset }, () => null),
    ...Array.from({ length: count }, (_, index) => {
      const date = `${props.month}-${String(index + 1).padStart(2, '0')}`
      return { date, day: index + 1, reward: rewards.get(date) }
    }),
  ]
})
const weekdays = computed(() =>
  Array.from({ length: 7 }, (_, index) =>
    new Intl.DateTimeFormat(locale.value, {
      weekday: 'short',
      timeZone: 'UTC',
    }).format(new Date(Date.UTC(2026, 0, 5 + index)))
  )
)
</script>
<template>
  <div :aria-label="t('restored.calendar')">
    <div
      class="grid grid-cols-7 gap-1 text-center text-xs text-[var(--text-tertiary)]"
    >
      <span v-for="day in weekdays" :key="day" class="py-2">{{ day }}</span>
    </div>
    <div class="grid grid-cols-7 gap-1.5">
      <div
        v-for="(day, index) in days"
        :key="day?.date ?? index"
        class="relative flex min-h-10 items-center justify-center rounded-lg text-sm"
        :class="
          day?.reward !== undefined
            ? 'border border-[var(--accent)] bg-[var(--accent-soft)] font-bold text-[var(--accent-text)]'
            : 'bg-[var(--surface-muted)] text-[var(--text-secondary)]'
        "
        :data-checkin-record="day?.reward !== undefined ? day.date : undefined"
        :aria-label="
          day
            ? day.reward !== undefined
              ? t('restored.checkinDay', {
                  date: day.date,
                  quota: formatQuota(day.reward),
                })
              : day.date
            : undefined
        "
      >
        <span>{{ day?.day }}</span
        ><span
          v-if="day?.reward !== undefined"
          class="absolute bottom-0 right-1 text-[0.625rem]"
          aria-hidden="true"
          >✓</span
        >
      </div>
    </div>
    <p class="mt-3 text-xs leading-5 text-[var(--text-tertiary)]">
      {{ t('restored.calendarHint') }}
    </p>
  </div>
</template>
