<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import type { AdminChannel } from '@/types/console'
interface ProviderStatus {
  sampled_at: number
  version: string
  endpoints: Record<string, { status: number; error?: string }>
  models: Array<{ id: string; root: string; max_model_len: number | null }>
  metrics: Array<{
    name: string
    labels: Record<string, string>
    value: number
  }>
  raw_metrics: string
}
const props = defineProps<{ channel: AdminChannel }>()
const { t, locale } = useI18n()
const data = ref<ProviderStatus | null>(null)
const busy = ref(false)
const error = ref('')
let controller: AbortController | undefined
async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  busy.value = true
  error.value = ''
  try {
    const result = await api.get<ProviderStatus>(
      `/api/channel/${props.channel.id}/${props.channel.type === 62 ? 'vllm' : 'sglang'}/status`,
      undefined,
      { signal: request.signal }
    )
    if (!request.signal.aborted) data.value = result
  } catch (cause) {
    if (!request.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!request.signal.aborted) busy.value = false
  }
}
watch(
  () => props.channel.id,
  () => {
    data.value = null
    void load()
  },
  { immediate: true }
)
onBeforeUnmount(() => controller?.abort())
</script>
<template>
  <section
    class="mt-4 space-y-3 border-t border-[var(--border-subtle)] pt-4 text-sm"
    :aria-busy="busy"
  >
    <header class="flex flex-wrap items-center justify-between gap-3">
      <h3 class="font-semibold">
        {{ t('channels.providerStatus') }} ·
        {{ channel.type === 62 ? 'vLLM' : 'SGLang' }}
      </h3>
      <ConsoleButton
        variant="secondary"
        size="sm"
        :disabled="busy"
        @click="load"
        >{{ t('common.refresh') }}</ConsoleButton
      >
    </header>
    <p v-if="error" role="alert" class="text-[var(--status-danger-text)]">
      {{ error }}
    </p>
    <template v-if="data">
      <p>
        {{ t('channels.providerVersion') }}: {{ data.version || '—' }} ·
        {{ new Date(data.sampled_at).toLocaleString(locale) }}
      </p>
      <dl class="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
        <template v-for="(endpoint, path) in data.endpoints" :key="path"
          ><dt class="break-all font-mono">{{ path }}</dt>
          <dd :class="endpoint.error ? 'text-[var(--status-danger-text)]' : ''">
            {{ endpoint.status || '—'
            }}{{
              endpoint.error
                ? ` · ${t(`channels.providerErrors.${endpoint.error}`)}`
                : ''
            }}
          </dd></template
        >
      </dl>
      <div class="overflow-x-auto">
        <table class="w-full text-left text-xs">
          <thead>
            <tr>
              <th>{{ t('channels.models') }}</th>
              <th>{{ t('channels.contextWindow') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="model in data.models"
              :key="model.id"
              class="border-t border-[var(--border-subtle)]"
            >
              <td class="break-all py-2 font-mono">
                {{ model.id
                }}<span
                  v-if="model.root && model.root !== model.id"
                  class="block text-[var(--text-tertiary)]"
                  >{{ model.root }}</span
                >
              </td>
              <td>{{ model.max_model_len ?? '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <details v-if="data.metrics.length">
        <summary>
          {{ t('channels.providerMetrics') }} ({{ data.metrics.length }})
        </summary>
        <dl class="mt-2 space-y-2">
          <div
            v-for="(metric, index) in data.metrics"
            :key="index"
            class="break-all font-mono text-xs"
          >
            <dt>{{ metric.name }} {{ JSON.stringify(metric.labels) }}</dt>
            <dd>{{ metric.value }}</dd>
          </div>
        </dl>
      </details>
      <details v-if="data.raw_metrics">
        <summary>{{ t('channels.providerRawMetrics') }}</summary>
        <pre
          class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs"
          >{{ data.raw_metrics }}</pre>
      </details>
    </template>
  </section>
</template>
