<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RoutingSnapshot } from '@/api/opsMonitor'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { formatCount, formatLatencyMs, formatPercent } from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import OpsMetric from './OpsMetric.vue'

const props = defineProps<{
  snapshot: RoutingSnapshot | null
  loading: boolean
  error: string
  updatedAt: number
}>()
const emit = defineEmits<{ retry: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})
const models = computed(() =>
  [...(props.snapshot?.observations ?? [])]
    .sort((a, b) => b.samples - a.samples)
    .slice(0, 6)
)
const samples = computed(
  () =>
    props.snapshot?.observations.reduce((sum, row) => sum + row.samples, 0) ??
    null
)
const known = computed(
  () => props.snapshot?.observations.filter((row) => row.known).length ?? null
)
const latencyKnown = computed(
  () =>
    props.snapshot?.observations.filter((row) => row.latency_known).length ??
    null
)
const peak = computed(() =>
  Math.max(1, ...models.value.map((row) => row.samples))
)
</script>

<template>
  <OpsCard
    :title="t('quality.title')"
    :scope="t('native.observationScope')"
    :loading="loading"
    :loaded="snapshot !== null"
    :error="error"
    :updated-at="updatedAt"
    data-ops-section="quality"
    @retry="emit('retry')"
  >
    <template v-if="snapshot">
      <p class="ops-note">{{ t('native.observationNote') }}</p>
      <dl class="ops-metric-grid ops-metric-grid--4">
        <OpsMetric
          :label="t('native.sampleCount')"
          :value="formatCount(samples, locale)"
          size="lg"
        />
        <OpsMetric
          :label="t('native.knownObservations')"
          :value="formatCount(known, locale)"
          size="lg"
        />
        <OpsMetric
          :label="t('native.latencyObservations')"
          :value="formatCount(latencyKnown, locale)"
          size="lg"
        />
        <OpsMetric
          :label="t('quality.throughput')"
          value="--"
          :hint="t('native.notCollected')"
          size="lg"
        />
      </dl>
      <div class="ops-trend">
        <p class="ops-trend-title">
          {{ t('quality.strip')
          }}<span class="ops-scope">{{ t('scope.perHour') }}</span>
        </p>
        <p class="ops-empty ops-unavailable-panel" data-ops-history-unavailable>
          {{ t('native.requestHistoryUnavailable') }}
        </p>
      </div>
      <div class="ops-trend">
        <p class="ops-trend-title">{{ t('native.topObservations') }}</p>
        <p v-if="!models.length" class="ops-empty">
          {{ t('native.noObservations') }}
        </p>
        <div
          v-else
          class="ops-table-scroll"
          tabindex="0"
          :aria-label="t('native.topObservations')"
        >
          <table class="ops-model-table" data-ops-top-models>
            <thead>
              <tr>
                <th scope="col">{{ t('quality.model') }}</th>
                <th scope="col">{{ t('native.sampleCount') }}</th>
                <th scope="col">{{ t('native.errorEWMA') }}</th>
                <th scope="col">{{ t('native.latencyEWMA') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="model in models"
                :key="`${model.channel_id}:${model.model}:${model.operation}:${model.stream}`"
              >
                <th scope="row">
                  <span class="ops-model-name" :title="model.model">{{
                    model.model || t('quality.unnamedModel')
                  }}</span>
                  <span class="ops-footnote"
                    >#{{ model.channel_id }} ·
                    {{
                      model.operation === null
                        ? t('native.unknownOperation')
                        : t('native.operation', { id: model.operation })
                    }}
                    ·
                    {{
                      model.stream
                        ? t('native.streaming')
                        : t('native.nonStreaming')
                    }}</span
                  >
                  <span class="ops-model-bar" aria-hidden="true"
                    ><span
                      :style="{ width: `${(model.samples / peak) * 100}%` }"
                  /></span>
                </th>
                <td>{{ formatCount(model.samples, locale) }}</td>
                <td>
                  {{
                    formatPercent(
                      model.known ? model.error_rate * 100 : null,
                      locale
                    ).value
                  }}<span v-if="model.known" class="ops-unit">%</span>
                </td>
                <td>
                  {{
                    formatLatencyMs(
                      model.latency_known ? model.latency_ms : null,
                      locale
                    ).value
                  }}<span class="ops-unit">{{
                    formatLatencyMs(
                      model.latency_known ? model.latency_ms : null,
                      locale
                    ).unit
                  }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>
  </OpsCard>
</template>
