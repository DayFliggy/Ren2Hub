<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { modelMarketMessages } from '@/i18n/modelMarket'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })
</script>
<template>
  <ConsoleCard
    :title="t('channelsUnavailable')"
    data-channel-shelves-unavailable
  >
    <p class="text-sm leading-7 text-[var(--text-secondary)]" role="status">
      {{ t('channelsHint') }}
    </p>
    <p class="mt-3 text-xs text-[var(--text-tertiary)]">
      {{ t('channelMetricsUnavailable') }}
    </p>
  </ConsoleCard>
</template>
