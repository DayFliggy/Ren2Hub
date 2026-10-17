<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { AdminChannel } from '@/types/console'
import {
  adminChannelTypeMeta,
  adminChannelResponseText,
} from '@/constants/adminChannels'
import { formatQuota, formatMoney } from '@/utils/format'
import ChannelStatusBadge from './ChannelStatusBadge.vue'

defineProps<{
  rows: AdminChannel[]
  selected: number[]
  selectable: boolean
  disabled: boolean
}>()
const emit = defineEmits<{ select: [channel: AdminChannel] }>()
const { t } = useI18n()
</script>

<template>
  <div class="grid gap-3 p-3">
    <article
      v-for="channel in rows"
      :key="channel.id"
      class="min-w-0 rounded-xl border border-[var(--border-subtle)] p-4"
    >
      <div class="flex items-start justify-between gap-3">
        <label class="flex min-w-0 items-start gap-2">
          <input
            v-if="selectable"
            type="checkbox"
            :checked="selected.includes(channel.id)"
            :disabled="disabled"
            :aria-label="t('channels.selectChannel', { name: channel.name })"
            @change="emit('select', channel)"
          />
          <span class="min-w-0 break-words font-medium"
            >{{ channel.name
            }}<small class="ml-2 text-[var(--text-tertiary)]"
              >#{{ channel.id }}</small
            ></span
          >
        </label>
        <ChannelStatusBadge :channel="channel" />
      </div>
      <dl class="mt-3 grid grid-cols-2 gap-3 text-sm">
        <div>
          <dt class="text-[var(--text-tertiary)]">{{ t('channels.type') }}</dt>
          <dd>{{ adminChannelTypeMeta(channel.type).label }}</dd>
        </div>
        <div class="min-w-0">
          <dt class="text-[var(--text-tertiary)]">{{ t('channels.group') }}</dt>
          <dd class="break-words">{{ channel.group }}</dd>
        </div>
        <div>
          <dt class="text-[var(--text-tertiary)]">{{ t('channels.usage') }}</dt>
          <dd>{{ formatQuota(channel.used_quota) }}</dd>
        </div>
        <div>
          <dt class="text-[var(--text-tertiary)]">
            {{ t('channels.upstreamBalance') }}
          </dt>
          <dd>{{ formatMoney(channel.balance) }}</dd>
        </div>
        <div>
          <dt class="text-[var(--text-tertiary)]">
            {{ t('channels.priority') }} / {{ t('channels.weight') }}
          </dt>
          <dd>{{ channel.priority }} / {{ channel.weight }}</dd>
        </div>
        <div>
          <dt class="text-[var(--text-tertiary)]">
            {{ t('channels.response') }}
          </dt>
          <dd>
            {{
              adminChannelResponseText(
                channel.response_time,
                t('channels.notTested')
              )
            }}
          </dd>
        </div>
      </dl>
      <div class="mt-3 flex flex-wrap gap-2">
        <slot name="actions" :channel="channel" />
      </div>
    </article>
  </div>
</template>
