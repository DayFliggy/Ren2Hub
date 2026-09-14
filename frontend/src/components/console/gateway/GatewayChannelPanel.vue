<script setup lang="ts">
import { ref, watch } from 'vue'
import { RefreshCw } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { gatewayApi } from '@/api/gateway'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useAuthStore } from '@/stores/auth'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import GatewayConfigPanel from './GatewayConfigPanel.vue'
import type { GatewayChannelRuntime, GatewayRateRuntime } from '@/types/gateway'
const props = defineProps<{ channelId: number }>()
const { t } = useI18n()
const auth = useAuthStore()
const request = useLatestRequest()
const credentialRequest = useLatestRequest()
const runtime = ref<GatewayChannelRuntime | null>(null)
const credentials = ref<string[]>([])
const selected = ref('')
const credentialRuntime = ref<GatewayRateRuntime | null>(null)
const error = ref('')
const credentialError = ref('')
const loading = ref(false)
const metrics = [
  'rpm_used',
  'tokens_used',
  'tokens_reserved',
  'cooldown_ms',
] as const
async function load() {
  loading.value = true
  runtime.value = null
  error.value = ''
  const result = await request.run(async (signal) =>
    Promise.allSettled([
      gatewayApi.channelRuntime(props.channelId, signal),
      gatewayApi.credentials(props.channelId, signal),
    ])
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    error.value = String(result.error)
    return
  }
  const [live, ids] = result.value
  if (live.status === 'fulfilled') runtime.value = live.value
  else error.value = t('gateway.runtimeUnavailable')
  if (ids.status === 'fulfilled') credentials.value = ids.value
  else error.value = String(ids.reason)
  if (selected.value) void loadCredential()
}
async function loadCredential() {
  credentialRuntime.value = null
  credentialError.value = ''
  credentialRequest.cancel()
  if (!selected.value) return
  const result = await credentialRequest.run((signal) =>
    gatewayApi.credentialRuntime(selected.value, signal)
  )
  if (result.stale) return
  if (result.ok) credentialRuntime.value = result.value
  else credentialError.value = t('gateway.runtimeUnavailable')
}
watch(selected, loadCredential)
watch(
  () => props.channelId,
  () => {
    selected.value = ''
    credentials.value = []
    void load()
  },
  { immediate: true }
)
</script>

<template>
  <section
    class="min-w-0 space-y-4 border-t border-[var(--border-subtle)] py-4"
    data-testid="gateway-channel-panel"
  >
    <div class="flex items-center justify-between gap-3">
      <h3 class="text-sm font-semibold">{{ t('gateway.channelRuntime') }}</h3>
      <ConsoleButton
        variant="ghost"
        size="sm"
        :disabled="loading"
        :title="t('gateway.reload')"
        :aria-label="t('gateway.reload')"
        @click="load"
        ><RefreshCw class="h-4 w-4"
      /></ConsoleButton>
    </div>
    <p
      v-if="error"
      role="alert"
      class="break-words text-sm text-[var(--status-warning-text)]"
    >
      {{ error }}
    </p>
    <p v-if="loading" class="text-sm text-[var(--text-tertiary)]">
      {{ t('common.loading') }}
    </p>
    <div v-if="runtime" class="space-y-3">
      <div class="flex flex-wrap gap-5 text-sm">
        <span
          >{{ t('gateway.inFlight') }}
          <strong
            >{{ runtime.in_flight }} / {{ runtime.capacity }}</strong
          ></span
        ><span
          >{{ t('gateway.queued') }} <strong>{{ runtime.queued }}</strong></span
        >
      </div>
      <dl class="grid gap-3 sm:grid-cols-2">
        <div v-for="metric in metrics" :key="metric">
          <dt class="text-xs text-[var(--text-tertiary)]">
            {{ t(`gateway.${metric}`) }}
          </dt>
          <dd class="text-sm font-medium">{{ runtime.rate[metric] }}</dd>
        </div>
      </dl>
    </div>
    <select
      v-model="selected"
      :aria-label="t('gateway.credentialID')"
      class="h-10 w-full min-w-0 rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3 font-mono text-xs text-[var(--text-primary)] focus-ring"
    >
      <option value="">{{ t('gateway.selectCredential') }}</option>
      <option v-for="id in credentials" :key="id" :value="id">{{ id }}</option>
    </select>
    <p
      v-if="credentialError"
      role="status"
      class="text-sm text-[var(--status-warning-text)]"
    >
      {{ credentialError }}
    </p>
    <dl v-if="credentialRuntime" class="grid gap-3 sm:grid-cols-2">
      <div v-for="metric in metrics" :key="metric">
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t(`gateway.${metric}`) }}
        </dt>
        <dd class="text-sm font-medium">{{ credentialRuntime[metric] }}</dd>
      </div>
    </dl>
    <GatewayConfigPanel
      v-if="auth.isRoot"
      :channel-id="channelId"
      class="border-t border-[var(--border-subtle)] pt-4"
    />
  </section>
</template>
