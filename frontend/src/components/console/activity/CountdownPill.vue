<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  end: number // epoch seconds
}>()

const { t } = useI18n()
const now = ref(Math.floor(Date.now() / 1000))
let timer: ReturnType<typeof setInterval> | null = null

const remain = computed(() => Math.max(0, props.end - now.value))

const parts = computed(() => {
  const s = remain.value
  return {
    days: Math.floor(s / 86_400),
    hours: Math.floor((s % 86_400) / 3600),
    minutes: Math.floor((s % 3600) / 60),
    seconds: s % 60,
  }
})

const label = computed(() => {
  if (remain.value <= 0) return t('activity.countdown.ended')
  const p = parts.value
  const hh = String(p.hours).padStart(2, '0')
  const mm = String(p.minutes).padStart(2, '0')
  const ss = String(p.seconds).padStart(2, '0')
  if (p.days > 0) {
    return t('activity.countdown.untilEnd', {
      days: p.days,
      time: `${hh}:${mm}:${ss}`,
    })
  }
  return t('activity.countdown.untilEndShort', { time: `${hh}:${mm}:${ss}` })
})

onMounted(() => {
  timer = setInterval(() => {
    now.value = Math.floor(Date.now() / 1000)
  }, 1000)
})
onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <span
    class="inline-flex items-center gap-1 rounded-full bg-[var(--surface-muted)] px-2.5 py-1 text-xs font-medium text-[var(--text-tertiary)]"
  >
    <svg
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
    >
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 3" />
    </svg>
    {{ label }}
  </span>
</template>
