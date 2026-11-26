<script setup lang="ts">
import { Plus, RefreshCw } from 'lucide-vue-next'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, type RouteLocationRaw } from 'vue-router'

import { UPSTREAM_PLATFORMS, type UpstreamAccount } from '@/api/upstreams'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import IconButton from '@/components/common/IconButton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { relativeTime } from '@/utils/format'
import {
  accountAlert,
  accountStatusText,
  accountTone,
  balanceText,
  siteHost,
} from '../lib/upstreamText'
import type { UpstreamWorkspaceTab } from './UpstreamWorkspace.vue'

const keyword = defineModel<string>('keyword', { required: true })
const platform = defineModel<string>('platform', { required: true })
const abnormalOnly = defineModel<boolean>('abnormalOnly', { required: true })
const props = defineProps<{
  /** Accounts left after the filters. */
  accounts: UpstreamAccount[]
  total: number
  abnormal: number
  selectedId: number | null
  /** The selected account exists but the filters hide it. */
  selectedHidden: boolean
  tab: UpstreamWorkspaceTab
  loading: boolean
  refreshing: boolean
  canAdd: boolean
  /** Desktop switches sites in place; narrow screens navigate. */
  replace: boolean
}>()
const emit = defineEmits<{ refresh: []; add: []; clearFilters: [] }>()
const { t, te, locale } = useI18n()

const list = ref<HTMLElement | null>(null)
const filtered = computed(
  () => !!keyword.value.trim() || !!platform.value || abnormalOnly.value
)
const platformOptions = computed(() => [
  { value: '', label: t('upstreams.allPlatforms') },
  ...UPSTREAM_PLATFORMS.map((value) => ({
    value,
    label: t(`upstreams.platforms.${value}`),
  })),
])
const rows = computed(() =>
  props.accounts.map((account) => {
    const alert = accountAlert(account, t, te)
    return {
      account,
      alert,
      host: siteHost(account.base_url),
      balance: balanceText(account, t),
      label: t('upstreams.siteLabel', {
        name: account.name,
        status: alert?.title ?? accountStatusText(account.status, t, te),
      }),
    }
  })
)

function linkTo(account: UpstreamAccount): RouteLocationRaw {
  return {
    name: 'upstreams',
    query: {
      account: String(account.id),
      ...(props.tab !== 'keys' ? { tab: props.tab } : {}),
    },
  }
}

function syncText(account: UpstreamAccount) {
  return account.last_sync_at
    ? relativeTime(account.last_sync_at, locale.value)
    : t('upstreams.neverSynced')
}

// Keep a deep-linked or newly selected site in view.
watch(
  () => [props.selectedId, props.loading] as const,
  async () => {
    await nextTick()
    list.value
      ?.querySelector('[aria-current="page"]')
      ?.scrollIntoView({ block: 'nearest' })
  },
  { immediate: true }
)
</script>

<template>
  <div
    class="flex min-h-0 flex-1 flex-col bg-[var(--surface-container-low)] lg:rounded-l-2xl"
  >
    <header class="space-y-3 border-b border-[var(--border-subtle)] px-4 py-3">
      <div class="flex items-center gap-2">
        <h1
          class="display-title min-w-0 flex-1 truncate text-base font-bold text-[var(--text-primary)]"
        >
          {{ t('upstreams.sites') }}
        </h1>
        <IconButton
          :label="t('upstreams.refresh')"
          :disabled="refreshing || loading"
          data-upstream-refresh
          @click="emit('refresh')"
        >
          <RefreshCw
            :size="15"
            :class="{ 'animate-spin': refreshing }"
            aria-hidden="true"
          />
        </IconButton>
        <ConsoleButton
          size="sm"
          :disabled="!canAdd"
          :aria-label="t('upstreams.add')"
          data-upstream-add
          @click="emit('add')"
        >
          <Plus :size="14" aria-hidden="true" />
          {{ t('upstreams.addShort') }}
        </ConsoleButton>
      </div>
      <div class="flex min-h-8 flex-wrap items-center justify-between gap-2">
        <p
          class="text-xs tabular-nums text-[var(--text-tertiary)]"
          aria-live="polite"
          data-upstream-summary
        >
          {{
            filtered
              ? t('upstreams.siteFiltered', {
                  visible: accounts.length,
                  total,
                })
              : t('upstreams.siteSummary', { total })
          }}
        </p>
        <ConsoleButton
          v-if="abnormal > 0 || abnormalOnly"
          variant="ghost"
          size="sm"
          :aria-pressed="abnormalOnly"
          :class="
            abnormalOnly
              ? 'border border-[var(--accent)] text-[var(--accent-text)]'
              : ''
          "
          data-upstream-abnormal
          @click="abnormalOnly = !abnormalOnly"
        >
          {{ t('upstreams.abnormalOnly', { count: abnormal }) }}
        </ConsoleButton>
      </div>
      <SearchInput
        v-model="keyword"
        :placeholder="t('upstreams.search')"
        :aria-label="t('upstreams.search')"
        name="upstream-search"
        class="w-full"
      />
      <SegmentedToggle
        v-model="platform"
        :options="platformOptions"
        :label="t('upstreams.platformFilter')"
        size="sm"
      />
    </header>

    <button
      v-if="selectedHidden"
      type="button"
      class="border-b border-[var(--border-subtle)] px-4 py-2 text-left text-xs text-[var(--text-secondary)] hover:bg-[var(--state-hover-layer)] focus-ring"
      data-upstream-selected-hidden
      @click="emit('clearFilters')"
    >
      {{ t('upstreams.selectedHidden') }}
    </button>

    <div
      ref="list"
      class="subtle-scroll min-h-0 flex-1 lg:overflow-y-auto"
      :aria-busy="loading"
      data-upstream-site-list
    >
      <div v-if="loading" class="space-y-1 p-2">
        <SkeletonBlock
          v-for="index in 6"
          :key="index"
          rounded="md"
          class="h-16"
        />
      </div>
      <div
        v-else-if="!accounts.length"
        class="flex flex-col items-center gap-3 px-4 py-8"
      >
        <EmptyState :title="t('upstreams.noMatch')" />
        <ConsoleButton
          size="sm"
          variant="ghost"
          data-upstream-clear-filters
          @click="emit('clearFilters')"
        >
          {{ t('upstreams.clearFilters') }}
        </ConsoleButton>
      </div>
      <ul v-else role="list">
        <li
          v-for="row in rows"
          :key="row.account.id"
          class="border-b border-[var(--border-subtle)]"
        >
          <RouterLink
            :to="linkTo(row.account)"
            :replace="replace"
            :aria-label="row.label"
            :aria-current="row.account.id === selectedId ? 'page' : undefined"
            class="relative block px-4 py-3 transition-colors focus-ring"
            :class="
              row.account.id === selectedId
                ? 'bg-[var(--accent-soft)] before:absolute before:inset-y-0 before:left-0 before:w-[3px] before:bg-[var(--accent)]'
                : 'hover:bg-[var(--state-hover-layer)]'
            "
            data-upstream-site
          >
            <span class="flex items-baseline justify-between gap-3">
              <span
                class="min-w-0 truncate text-sm font-semibold"
                :class="
                  row.account.id === selectedId
                    ? 'text-[var(--accent-text)]'
                    : 'text-[var(--text-primary)]'
                "
              >
                {{ row.account.name }}
              </span>
              <span
                class="shrink-0 text-xs tabular-nums text-[var(--text-secondary)]"
                :title="row.balance.hint"
              >
                {{ row.balance.text }}
              </span>
            </span>
            <span
              class="mt-1 flex items-center gap-2 text-xs text-[var(--text-tertiary)]"
            >
              <span class="min-w-0 truncate" :title="row.account.base_url">
                {{ row.host }}
              </span>
              <span class="shrink-0 text-[var(--text-secondary)]">
                {{ t(`upstreams.platforms.${row.account.platform}`) }}
              </span>
              <span v-if="row.account.has_proxy" class="shrink-0">
                {{ t('upstreams.proxyBadge') }}
              </span>
              <span class="ml-auto shrink-0 tabular-nums">
                {{ syncText(row.account) }}
              </span>
            </span>
            <span
              v-if="row.alert"
              class="mt-1.5 flex min-w-0 items-center gap-2 text-xs"
              data-upstream-site-alert
            >
              <StatusChip
                v-if="row.account.status !== 'active'"
                :tone="accountTone(row.account.status)"
                class="shrink-0"
              >
                {{ accountStatusText(row.account.status, t, te) }}
              </StatusChip>
              <span
                class="min-w-0 truncate"
                :class="
                  row.alert.tone === 'danger'
                    ? 'text-[var(--status-danger-text)]'
                    : 'text-[var(--text-secondary)]'
                "
                :title="row.alert.detail || row.alert.title"
              >
                {{ row.alert.detail || row.alert.title }}
              </span>
            </span>
          </RouterLink>
        </li>
      </ul>
    </div>
  </div>
</template>
