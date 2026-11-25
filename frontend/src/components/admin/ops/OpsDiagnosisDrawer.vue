<script setup lang="ts">
import { computed, inject } from 'vue'
import { useI18n } from 'vue-i18n'
import { routerKey } from 'vue-router'
import type {
  OpsCheck,
  RouteBreakerSnapshot,
  RouteCooldownSnapshot,
} from '@/api/opsMonitor'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import {
  STATUS_TONES,
  durationParts,
  formatLatencyMs,
  formatPercent,
} from '@/utils/opsFormat'

const props = defineProps<{
  open: boolean
  checks: OpsCheck[]
  breakers: RouteBreakerSnapshot[]
  cooldowns: RouteCooldownSnapshot[]
  channelNames: Record<number, string>
  nowMs: number
}>()

const emit = defineEmits<{ close: [] }>()
const router = inject(routerKey, null)
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})

const hasIssues = computed(
  () =>
    props.checks.length > 0 ||
    props.breakers.length > 0 ||
    props.cooldowns.length > 0
)

function checkValue(check: OpsCheck, value: number): string {
  if (
    check.key === 'database' ||
    check.key === 'log_database' ||
    check.key === 'redis'
  ) {
    const latency = formatLatencyMs(value, locale.value)
    return `${latency.value} ${latency.unit}`
  }
  return `${formatPercent(value, locale.value).value}%`
}

function checkDetail(check: OpsCheck): string {
  if (check.reason) return t(`observationReasons.${check.reason}`)
  if (check.value === null) return t(`status.${check.status}`)
  const value = checkValue(check, check.value)
  if (check.threshold === null || check.threshold <= 0) return value
  return `${value} / ${checkValue(check, check.threshold)}`
}

function checkImpact(check: OpsCheck): string {
  if (
    check.key === 'database' ||
    check.key === 'log_database' ||
    check.key === 'redis'
  ) {
    return t('diagnosis.impactCheckStorage')
  }
  return t('diagnosis.impactCheckResource')
}

function formatRemaining(untilMs: number): string {
  const remainingSeconds = Math.ceil((untilMs - props.nowMs) / 1000)
  if (remainingSeconds <= 0) return t('upstreamLive.readyToProbe')
  const duration = durationParts(remainingSeconds)
    .map((part) => t(`duration.${part.unit}`, { n: part.amount }))
    .join(' ')
  return t('upstreamLive.remaining', { duration })
}

function channelDisplay(id: number, name?: string): string {
  return name || props.channelNames[id] || t('upstream.channelId', { id })
}

function navigateTo(path: string) {
  emit('close')
  if (router) void router.push(path)
}
</script>

<template>
  <ConsoleModal
    :open="open"
    :title="t('diagnosis.title')"
    presentation="drawer"
    drawer-side="right"
    size="lg"
    @close="emit('close')"
  >
    <div data-ops-diagnosis-modal>
      <p class="ops-note mb-4">{{ t('diagnosis.subtitle') }}</p>

      <p v-if="!hasIssues" class="ops-empty">{{ t('diagnosis.empty') }}</p>

      <div v-else class="ops-diagnosis-groups">
        <section v-if="checks.length" class="ops-diagnosis-group">
          <h3 class="ops-trend-title">{{ t('diagnosis.sectionChecks') }}</h3>
          <ul class="ops-diagnosis-list">
            <li
              v-for="check in checks"
              :key="check.key"
              class="ops-diagnosis-item"
            >
              <div class="ops-diagnosis-item-head">
                <strong>{{ t(`checks.${check.key}`) }}</strong>
                <StatusChip :tone="STATUS_TONES[check.status]">{{
                  t(`status.${check.status}`)
                }}</StatusChip>
              </div>
              <p class="ops-diagnosis-item-detail">{{ checkDetail(check) }}</p>
              <p class="ops-diagnosis-item-impact">
                {{ t('diagnosis.impact', { text: checkImpact(check) }) }}
              </p>
            </li>
          </ul>
        </section>

        <section v-if="breakers.length" class="ops-diagnosis-group">
          <h3 class="ops-trend-title">{{ t('diagnosis.sectionBreakers') }}</h3>
          <ul class="ops-diagnosis-list">
            <li
              v-for="item in breakers"
              :key="`${item.channel_id}:${item.model}`"
              class="ops-diagnosis-item"
            >
              <div class="ops-diagnosis-item-head">
                <strong>{{
                  channelDisplay(item.channel_id, item.channel_name)
                }}</strong>
                <div class="ops-diagnosis-chips">
                  <StatusChip
                    :tone="item.state === 'open' ? 'danger' : 'warning'"
                    >{{
                      item.state === 'open'
                        ? t('upstreamLive.stateOpen')
                        : t('upstreamLive.stateHalfOpen')
                    }}</StatusChip
                  >
                  <StatusChip v-if="item.probe_in_flight" tone="info">{{
                    t('upstreamLive.probeInFlight')
                  }}</StatusChip>
                </div>
              </div>
              <p class="ops-cooling-meta">
                <code>{{ item.model }}</code>
                <span v-if="item.trigger">{{ item.trigger }}</span>
                <span v-if="item.last_status">{{
                  t('upstreamLive.httpStatus', { code: item.last_status })
                }}</span>
                <code v-if="item.last_error_code">{{
                  item.last_error_code
                }}</code>
                <span>{{ formatRemaining(item.open_until) }}</span>
              </p>
              <p class="ops-diagnosis-item-impact">
                {{
                  t('diagnosis.impact', {
                    text:
                      item.state === 'open'
                        ? t('diagnosis.impactBreakerOpen')
                        : t('diagnosis.impactBreakerHalfOpen'),
                  })
                }}
              </p>
            </li>
          </ul>
        </section>

        <section v-if="cooldowns.length" class="ops-diagnosis-group">
          <h3 class="ops-trend-title">{{ t('diagnosis.sectionCooldowns') }}</h3>
          <ul class="ops-diagnosis-list">
            <li
              v-for="item in cooldowns"
              :key="item.channel_id"
              class="ops-diagnosis-item"
            >
              <div class="ops-diagnosis-item-head">
                <strong>{{
                  channelDisplay(item.channel_id, item.channel_name)
                }}</strong>
                <StatusChip tone="warning">{{
                  t('upstreamLive.cooldown')
                }}</StatusChip>
              </div>
              <p class="ops-cooling-meta">
                <span v-if="item.source">{{
                  t('upstreamLive.source', { source: item.source })
                }}</span>
                <span>{{ formatRemaining(item.until) }}</span>
              </p>
              <p class="ops-diagnosis-item-impact">
                {{
                  t('diagnosis.impact', { text: t('diagnosis.impactCooldown') })
                }}
              </p>
            </li>
          </ul>
        </section>
      </div>
    </div>

    <template #footer>
      <div class="ops-diagnosis-footer">
        <ConsoleButton
          variant="secondary"
          size="sm"
          @click="navigateTo('/channels')"
          >{{ t('diagnosis.openChannels') }}</ConsoleButton
        >
        <ConsoleButton
          variant="secondary"
          size="sm"
          @click="navigateTo('/system-settings/operations')"
          >{{ t('diagnosis.openSettings') }}</ConsoleButton
        >
        <ConsoleButton variant="primary" size="sm" @click="emit('close')">{{
          t('diagnosis.close')
        }}</ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>
