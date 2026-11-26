<script setup lang="ts">
import { onMounted } from 'vue'
import { RefreshCw } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import EmptyState from '@/components/common/EmptyState.vue'
import IconButton from '@/components/common/IconButton.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import VendorRouteGroup from './VendorRouteGroup.vue'
import { useAutoRoute } from '@/composables/useAutoRoute'

const { t, locale } = useI18n()
const { loading, error, lastUpdated, vendorList, load } = useAutoRoute()

onMounted(() => void load())
</script>

<template>
  <div class="sibling-space-y-3 sm:sibling-space-y-4" data-auto-route-panel>
    <!-- Lab classification is for display, not execution priority. -->
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
    <p
      v-if="error && lastUpdated"
      role="status"
      class="text-xs text-[var(--status-warning-text)]"
    >
      {{ t('dashboard.autoRoute.stale') }}
    </p>

    <!-- skeleton -->
    <div v-if="loading" class="sibling-space-y-4">
      <SkeletonBlock v-for="i in 4" :key="i" rounded="2xl" class="h-32" />
    </div>

    <!-- Preserve the historical vendor cards with native catalog facts. -->
    <template v-else-if="vendorList.length">
      <VendorRouteGroup
        v-for="group in vendorList"
        :key="group.groupSlug"
        :group-slug="group.groupSlug"
        :group-name="group.groupName"
        :channels="group.channels"
        :active-count="group.activeCount"
      />
    </template>

    <EmptyState
      v-else-if="!error"
      :title="t('dashboard.autoRoute.emptyTitle')"
      :hint="t('dashboard.autoRoute.emptyHint')"
    />
  </div>
</template>
