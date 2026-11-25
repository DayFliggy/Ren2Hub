<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import KeyMeterNote from '@/components/console/keys/KeyMeterNote.vue'
import type { TokenSummary } from '@/types/console'
import { tokenMeter } from '@/utils/tokenMeter'

/**
 * Name plus the key's state and compact restriction summary, shared by table
 * rows and mobile cards.
 */
const props = withDefaults(
  defineProps<{
    token: TokenSummary
    now: number
    heading?: boolean
  }>(),
  { heading: false }
)

const { t } = useI18n()
const meter = computed(() => tokenMeter(props.token, props.now))

const restrictions = computed(() => {
  const parts: string[] = []
  const details: string[] = []
  if (props.token.model_limits.length > 0) {
    const label = t(
      'keys.limits.models',
      { count: props.token.model_limits.length },
      props.token.model_limits.length
    )
    parts.push(label)
    details.push(`${label}: ${props.token.model_limits.join(', ')}`)
  }
  if (props.token.ip_limits.length > 0) {
    const label = t('keys.limits.ips')
    parts.push(label)
    details.push(`${label}: ${props.token.ip_limits.join(', ')}`)
  }
  if (!props.token.allow_user_quota_query) {
    const label = t('keys.limits.noQuotaQuery')
    parts.push(label)
    details.push(label)
  }
  return {
    text: parts.join(' · '),
    title: details.join(' · '),
  }
})
</script>

<template>
  <div class="min-w-0" data-key-identity :data-key-state="meter.state">
    <component
      :is="heading ? 'h2' : 'span'"
      class="block truncate"
      :class="
        heading
          ? 'text-sm font-semibold text-[var(--text-primary)]'
          : 'font-medium'
      "
      :title="token.name"
    >
      {{ token.name }}
    </component>
    <p
      class="mt-0.5 truncate text-[0.6875rem] leading-4 text-[var(--text-tertiary)]"
      :title="restrictions.title || undefined"
    >
      <KeyMeterNote :token="token" :meter="meter" />
      <span v-if="restrictions.text" data-key-restrictions>
        · {{ restrictions.text }}</span
      >
    </p>
  </div>
</template>
