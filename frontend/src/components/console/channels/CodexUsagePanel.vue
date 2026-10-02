<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  providerUsageApi,
  type ProviderUsageSnapshot,
} from '@/api/providerUsage'
import ConsoleButton from '@/components/common/ConsoleButton.vue'

const props = withDefaults(
  defineProps<{ channelId: number; canOperate?: boolean }>(),
  { canOperate: false }
)
const { t, locale } = useI18n()
const data = ref<ProviderUsageSnapshot | null>(null)
const automatic = ref(false)
const busy = ref(false)
const error = ref('')
const now = ref(Date.now())
const cooldown = computed(
  () => !!data.value && data.value.next_refresh_at * 1000 > now.value
)
const observationState = computed(() =>
  data.value?.state === 'known' && data.value.expires_at * 1000 <= now.value
    ? 'stale'
    : (data.value?.state ?? 'unknown')
)
let controller: AbortController | undefined
const timer = window.setInterval(() => {
  now.value = Date.now()
}, 1000)

function formatTime(value: number | null): string {
  return value
    ? new Date(value * 1000).toLocaleString(locale.value)
    : t('providerUsage.unknown')
}

async function load(action: 'read' | 'refresh' | 'settings', enabled = false) {
  if (action !== 'read' && (!props.canOperate || busy.value)) return
  controller?.abort()
  const request = new AbortController()
  controller = request
  busy.value = true
  error.value = ''
  const channelID = props.channelId
  try {
    const result =
      action === 'read'
        ? await providerUsageApi.get(channelID, request.signal)
        : action === 'refresh'
          ? await providerUsageApi.refresh(channelID, request.signal)
          : await providerUsageApi.settings(channelID, enabled, request.signal)
    if (
      !request.signal.aborted &&
      controller === request &&
      channelID === props.channelId
    ) {
      data.value = result
      automatic.value = result.auto_refresh
    }
  } catch {
    if (!request.signal.aborted && controller === request) {
      error.value = t('providerUsage.requestFailed')
      automatic.value = data.value?.auto_refresh ?? false
    }
  } finally {
    if (!request.signal.aborted && controller === request) busy.value = false
  }
}

function changeAutomatic(event: Event) {
  const checkbox = event.target as HTMLInputElement
  const requestedValue = checkbox.checked
  checkbox.checked = automatic.value
  if (busy.value || !props.canOperate) return
  void load('settings', requestedValue)
}

watch(
  () => props.canOperate,
  (allowed, previous) => {
    if (!allowed && previous) void load('read')
  }
)
watch(
  () => props.channelId,
  () => {
    data.value = null
    automatic.value = false
    void load('read')
  },
  { immediate: true }
)
onBeforeUnmount(() => {
  controller?.abort()
  window.clearInterval(timer)
})
</script>

<template>
  <section
    class="mt-4 space-y-3 border-t border-[var(--border-subtle)] pt-4 text-sm"
    :aria-busy="busy"
  >
    <header class="flex flex-wrap items-center justify-between gap-3">
      <h3 class="font-semibold">{{ t('providerUsage.title') }}</h3>
      <ConsoleButton
        v-if="canOperate"
        variant="secondary"
        size="sm"
        :disabled="busy || cooldown"
        @click="load('refresh')"
        >{{ t('providerUsage.refresh') }}</ConsoleButton
      >
    </header>
    <p class="text-xs text-[var(--text-secondary)]">
      {{ t('providerUsage.observationOnly') }}
    </p>
    <p v-if="error" role="alert" class="text-[var(--status-danger-text)]">
      {{ error }}
    </p>
    <template v-if="data">
      <div class="flex flex-wrap gap-x-4 gap-y-2" aria-live="polite">
        <span
          :class="
            observationState === 'error'
              ? 'text-[var(--status-danger-text)]'
              : 'text-[var(--text-secondary)]'
          "
          >{{ t(`providerUsage.states.${observationState}`) }}</span
        >
        <span v-if="data.plan_type"
          >{{ t('providerUsage.plan') }}: {{ data.plan_type }}</span
        >
      </div>
      <p
        v-if="data.error_code"
        role="status"
        class="text-[var(--status-danger-text)]"
      >
        {{ t(`providerUsage.errors.${data.error_code}`) }}
      </p>
      <p v-if="!data.windows.length" class="text-[var(--text-secondary)]">
        {{ t('providerUsage.empty') }}
      </p>
      <div v-else class="grid gap-3 sm:grid-cols-2">
        <article
          v-for="window in data.windows"
          :key="window.name"
          class="min-w-0 rounded-lg border border-[var(--border-subtle)] p-3"
        >
          <h4 class="font-medium">
            {{ t(`providerUsage.windows.${window.name}`) }}
          </h4>
          <p class="mt-1">
            {{
              window.used_percent === null
                ? t('providerUsage.unknown')
                : t('providerUsage.used', {
                    value: new Intl.NumberFormat(locale, {
                      maximumFractionDigits: 3,
                    }).format(window.used_percent),
                  })
            }}
          </p>
          <p class="mt-2 break-words text-xs text-[var(--text-secondary)]">
            {{ t('providerUsage.resetAt') }}: {{ formatTime(window.reset_at) }}
          </p>
          <p
            v-if="window.limit_seconds !== null"
            class="mt-1 text-xs text-[var(--text-secondary)]"
          >
            {{ t('providerUsage.limit', { seconds: window.limit_seconds }) }}
          </p>
        </article>
      </div>
      <dl
        class="grid gap-x-4 gap-y-1 text-xs text-[var(--text-secondary)] sm:grid-cols-[auto_minmax(0,1fr)]"
      >
        <dt>{{ t('providerUsage.fetchedAt') }}</dt>
        <dd class="break-words">{{ formatTime(data.fetched_at) }}</dd>
        <dt>{{ t('providerUsage.lastAttempt') }}</dt>
        <dd class="break-words">{{ formatTime(data.last_attempt_at) }}</dd>
        <template v-if="cooldown"
          ><dt>{{ t('providerUsage.retryAt') }}</dt>
          <dd class="break-words">
            {{ formatTime(data.next_refresh_at) }}
          </dd></template
        >
      </dl>
      <label v-if="canOperate" class="flex items-start gap-2"
        ><input
          type="checkbox"
          class="mt-0.5"
          :checked="automatic"
          :disabled="busy"
          @change="changeAutomatic"
        /><span>{{ t('providerUsage.autoRefresh') }}</span></label
      >
      <p v-else class="text-[var(--text-secondary)]">
        {{
          t(
            data.auto_refresh
              ? 'providerUsage.autoEnabled'
              : 'providerUsage.autoDisabled'
          )
        }}
      </p>
    </template>
  </section>
</template>
