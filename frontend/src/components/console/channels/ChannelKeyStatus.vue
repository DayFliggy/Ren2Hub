<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusChip from '@/components/common/StatusChip.vue'
import {
  adminChannelStatusLabelKey,
  adminChannelStatusTone,
  multiKeyAvailability,
} from '@/constants/adminChannels'
import type { AdminChannel } from '@/types/console'

const props = defineProps<{ channel: AdminChannel; detailed?: boolean }>()
const { t } = useI18n()
const availability = computed(() => multiKeyAvailability(props.channel))
const tone = computed(() => {
  const value = availability.value
  if (!value || value.enabled === value.total) return 'neutral'
  return value.enabled === 0 ? 'danger' : 'warning'
})
</script>

<template>
  <div
    v-if="availability"
    class="inline-flex min-w-0 flex-col items-start gap-2"
    data-channel-key-status
  >
    <StatusChip :tone="tone">{{
      t('channels.keysAvailable', {
        enabled: availability.enabled,
        total: availability.total,
      })
    }}</StatusChip>
    <ul
      v-if="detailed && availability.disabled.length"
      class="grid gap-1"
      data-channel-disabled-keys
    >
      <li
        v-for="key in availability.disabled"
        :key="key.position"
        class="flex items-center gap-2"
      >
        <span>{{ t('channels.keyPosition', { position: key.position }) }}</span>
        <StatusChip :tone="adminChannelStatusTone(key.status)">{{
          t(adminChannelStatusLabelKey(key.status))
        }}</StatusChip>
      </li>
    </ul>
  </div>
</template>
