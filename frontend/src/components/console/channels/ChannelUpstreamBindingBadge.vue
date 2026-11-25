<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import type { UpstreamBinding } from '@/api/upstreams'
import StatusChip from '@/components/common/StatusChip.vue'

defineProps<{ binding: UpstreamBinding }>()
const { t, te } = useI18n()

function remoteStatusText(status: string) {
  const key = `upstreams.keys.statuses.${status}`
  return te(key) ? t(key) : status
}
</script>

<template>
  <div
    class="mt-1 flex flex-wrap items-center gap-1"
    data-channel-upstream-binding
  >
    <RouterLink
      :to="{
        name: 'upstreams',
        query: { account: String(binding.account_id), tab: 'bindings' },
      }"
      class="text-xs text-[var(--accent)] hover:underline"
      :title="
        t('upstreams.channelLink.badgeTitle', {
          account: binding.account_id,
          key: binding.remote_key_id,
        })
      "
    >
      {{
        t('upstreams.channelLink.badge', {
          account: binding.account_id,
          key: binding.remote_key_id,
        })
      }}
    </RouterLink>
    <StatusChip
      v-if="binding.stale"
      tone="warning"
      class="text-[0.625rem]"
      data-channel-upstream-stale
    >
      {{ t('upstreams.channelLink.stale') }}
    </StatusChip>
    <StatusChip
      v-else-if="binding.remote_status !== 'active'"
      tone="danger"
      class="text-[0.625rem]"
    >
      {{
        t('upstreams.channelLink.remoteInactive', {
          status: remoteStatusText(binding.remote_status),
        })
      }}
    </StatusChip>
    <StatusChip
      v-if="binding.ratio_sync_enabled && !binding.stale"
      tone="info"
      class="text-[0.625rem]"
    >
      {{ t('upstreams.channelLink.ratioSync') }}
    </StatusChip>
  </div>
</template>
