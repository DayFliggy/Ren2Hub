<script setup lang="ts">
import { onMounted } from 'vue'
import { RefreshCw } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import EmptyState from '@/components/common/EmptyState.vue'
import IconButton from '@/components/common/IconButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import { useAutoRoute } from '@/composables/useAutoRoute'

const { t, locale } = useI18n()
const { loading, error, lastUpdated, vendorList, load } = useAutoRoute()

onMounted(() => void load())
</script>

<template>
  <div class="sibling-space-y-3 sm:sibling-space-y-4" data-auto-route-panel>
    <!-- Header card: explains the per-lab optimum and hosts refresh. -->
    <div
      class="relative overflow-hidden rounded-xl border border-[var(--accent-soft)] bg-[var(--accent-soft)] px-4 py-3 sm:rounded-2xl"
    >
      <div class="relative flex items-start gap-3 sm:items-center">
        <div class="min-w-0 flex-1">
          <p class="text-sm text-[var(--text-secondary)]">
            {{ t('dashboard.autoRoute.subtitle') }}
          </p>
          <p
            v-if="lastUpdated"
            class="mt-0.5 text-xs text-[var(--text-tertiary)]"
          >
            {{
              t('dashboard.autoRoute.updated', {
                time: lastUpdated.toLocaleTimeString(locale, {
                  hour: '2-digit',
                  minute: '2-digit',
                }),
              })
            }}
          </p>
        </div>
        <IconButton
          class="shrink-0 bg-[var(--surface-solid)]"
          :disabled="loading"
          :label="t('dashboard.autoRoute.refresh')"
          @click="load"
        >
          <RefreshCw
            :size="13"
            :class="{ 'animate-spin': loading }"
            aria-hidden="true"
          />
        </IconButton>
      </div>
    </div>

    <ErrorBanner v-if="error" :message="error" @retry="load" />
    <!-- skeleton -->
    <div v-if="loading" class="sibling-space-y-4">
      <div
        v-for="i in 4"
        :key="i"
        class="h-32 animate-pulse rounded-2xl bg-[var(--surface-muted)]"
      />
    </div>

    <!-- Lab groups, each ranking its own optimum. -->
    <template v-else-if="vendorList.length">
      <ConsoleCard
        v-for="group in vendorList"
        :key="group.groupSlug"
        :title="group.groupName"
        data-route-group
        :data-route-group-slug="group.groupSlug"
      >
        <p class="mb-3 text-xs text-[var(--text-tertiary)]">
          {{
            t('dashboard.autoRoute.activeChannelCount', {
              active: group.activeCount,
              total: group.channels.length,
            })
          }}
        </p>
        <div
          v-for="channel in group.channels"
          :key="channel.id"
          class="flex flex-wrap items-center justify-between gap-3 border-t border-[var(--border-subtle)] py-3 text-sm"
        >
          <span>{{ channel.name }}</span
          ><span class="text-xs text-[var(--text-tertiary)]"
            >{{ t('channels.channelRatio') }} ×{{ channel.channel_ratio }} ·
            {{ channel.in_flight }} / {{ channel.max_concurrency || '∞' }}</span
          >
          <span
            v-if="!channel.eligible"
            class="text-xs text-[var(--status-warning-text)]"
            >{{ channel.filter_reason }}</span
          >
        </div>
      </ConsoleCard>
    </template>

    <EmptyState
      v-else
      :title="t('dashboard.autoRoute.emptyTitle')"
      :hint="t('dashboard.autoRoute.emptyHint')"
    />
  </div>
</template>
