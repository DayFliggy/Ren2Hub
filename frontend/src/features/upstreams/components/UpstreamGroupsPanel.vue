<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import type { UpstreamAccount } from '@/api/upstreams'
import StatusChip from '@/components/common/StatusChip.vue'

const props = defineProps<{ account: UpstreamAccount }>()
const { t } = useI18n()

function ratioText(ratio: string) {
  if (ratio === 'auto') return t('upstreams.detail.ratioAuto')
  return ratio === '' ? t('upstreams.detail.ratioUnknown') : `${ratio}×`
}

function groupName(id: number) {
  const group = props.account.upstream_groups.find((item) => item.id === id)
  return group?.name ?? t('upstreams.detail.subscriptionGroup', { id })
}
</script>

<template>
  <section class="@container">
    <div
      class="grid gap-5 @5xl:grid-cols-[minmax(0,2fr)_minmax(16rem,1fr)] @5xl:items-start"
    >
      <div class="min-w-0">
        <h3 class="mb-2 text-sm font-semibold text-[var(--text-primary)]">
          {{
            account.site_meta.upstream_group
              ? t('upstreams.detail.userGroup', {
                  name: account.site_meta.upstream_group,
                })
              : t('upstreams.detail.groups')
          }}
        </h3>
        <p
          v-if="!account.upstream_groups.length"
          class="text-sm text-[var(--text-tertiary)]"
        >
          {{ t('upstreams.detail.noGroups') }}
        </p>
        <ul
          v-else
          class="grid overflow-hidden rounded-xl border border-[var(--border-subtle)] @xl:grid-cols-2 @5xl:grid-cols-3"
          data-upstream-groups
        >
          <li
            v-for="group in account.upstream_groups"
            :key="`${group.id ?? ''}:${group.name}`"
            class="-mb-px flex min-w-0 items-center justify-between gap-3 border-b border-[var(--border-subtle)] px-3 py-2 text-sm"
          >
            <span class="min-w-0 truncate text-[var(--text-primary)]">
              {{ group.name }}
              <span
                v-if="group.platform"
                class="ml-1 text-xs text-[var(--text-tertiary)]"
              >
                {{ group.platform }}
              </span>
            </span>
            <span class="shrink-0 tabular-nums text-[var(--text-secondary)]">
              {{ ratioText(group.ratio) }}
            </span>
          </li>
        </ul>
      </div>
      <div class="min-w-0">
        <h3 class="mb-2 text-sm font-semibold text-[var(--text-primary)]">
          {{ t('upstreams.detail.subscriptions') }}
        </h3>
        <p
          v-if="!account.subscriptions.length"
          class="text-sm text-[var(--text-tertiary)]"
        >
          {{ t('upstreams.detail.noSubscriptions') }}
        </p>
        <ul v-else class="space-y-1 text-sm">
          <li
            v-for="item in account.subscriptions"
            :key="item.id"
            class="flex flex-wrap items-center gap-2"
          >
            <span class="text-[var(--text-primary)]">
              {{ groupName(item.upstream_group_id) }}
            </span>
            <StatusChip tone="neutral">{{ item.status }}</StatusChip>
            <span
              v-if="item.expires_at"
              class="text-xs text-[var(--text-tertiary)]"
            >
              {{ t('upstreams.detail.expiresAt') }}
              {{ item.expires_at }}
            </span>
          </li>
        </ul>
      </div>
    </div>
  </section>
</template>
