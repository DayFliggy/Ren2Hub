<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import type { TokenSummary } from '@/types/console'
import { formatQuota, formatTime } from '@/utils/format'
import type { TokenMeter } from '@/utils/tokenMeter'

/**
 * A key's full detail as label/value rows: the balance group, then expiry,
 * limits and access. Shared by the desktop popover and the mobile card, which
 * already shows the balance on its quota bar and so leaves that group out.
 */
const props = withDefaults(
  defineProps<{
    token: TokenSummary
    meter: TokenMeter
    withBalance?: boolean
  }>(),
  { withBalance: true }
)

const { t } = useI18n()

const MODEL_LIMIT = 3
const IP_LIMIT = 2

const expiry = computed(() =>
  props.token.expired_time < 0
    ? t('common.never')
    : formatTime(props.token.expired_time)
)

// Only a deadline within the warning window takes a colour.
const expiryColor = computed(() =>
  props.meter.expiresInDays !== null ? 'var(--status-warning-text)' : undefined
)

function limitText(items: string[], max: number): string {
  if (items.length === 0) return t('keys.meter.noLimit')
  const shown = items.slice(0, max).join(', ')
  return items.length > max
    ? `${shown} ${t('keys.meter.more', { count: items.length - max })}`
    : shown
}
</script>

<template>
  <div class="text-xs" data-key-quota-detail>
    <dl v-if="withBalance" class="key-detail-group">
      <dt>{{ t('keys.meter.remaining') }}</dt>
      <dd
        class="font-mono font-semibold"
        :class="{ 'line-through': meter.state === 'expired' }"
        :style="{ color: meter.remainingColor }"
      >
        {{
          token.unlimited
            ? t('common.unlimited')
            : formatQuota(token.remain_quota, 3)
        }}
      </dd>
      <dt>{{ t('keys.meter.usedTotal') }}</dt>
      <dd class="font-mono">{{ formatQuota(token.used_quota, 3) }}</dd>
      <template v-if="meter.usedPercent !== null">
        <dt>{{ t('keys.meter.usedShare') }}</dt>
        <dd class="font-mono">{{ meter.usedPercent }}%</dd>
      </template>
    </dl>
    <dl class="key-detail-group">
      <dt>{{ t('keys.colExpired') }}</dt>
      <dd
        class="font-mono"
        :style="expiryColor ? { color: expiryColor } : undefined"
      >
        {{ expiry }}
      </dd>
      <dt>{{ t('keys.colCreated') }}</dt>
      <dd class="font-mono">{{ formatTime(token.created_time) }}</dd>
      <dt>{{ t('keys.meter.models') }}</dt>
      <dd class="break-all">
        {{ limitText(token.model_limits, MODEL_LIMIT) }}
      </dd>
      <dt>{{ t('keys.meter.ips') }}</dt>
      <dd class="font-mono break-words">
        {{ limitText(token.ip_limits, IP_LIMIT) }}
      </dd>
      <dt>{{ t('keys.meter.quotaQuery') }}</dt>
      <dd>
        {{
          token.allow_user_quota_query
            ? t('keys.meter.allowed')
            : t('keys.meter.denied')
        }}
      </dd>
    </dl>
  </div>
</template>

<style scoped>
/* Values inherit the primary colour so a state class on one value wins. */
.key-detail-group {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  column-gap: 1rem;
  row-gap: 0.375rem;
  color: var(--text-primary);
}
.key-detail-group dt {
  color: var(--text-secondary);
}
.key-detail-group dd {
  text-align: right;
}
.key-detail-group + .key-detail-group {
  margin-top: 0.75rem;
  border-top: 1px solid var(--border-subtle);
  padding-top: 0.75rem;
}
</style>
