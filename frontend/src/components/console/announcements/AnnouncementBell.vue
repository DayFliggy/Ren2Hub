<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import {
  ArrowRight,
  Bell,
  CheckCheck,
  ChevronRight,
  Pin,
  RefreshCw,
  X,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import type { UserAnnouncement } from '@/api/announcements'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import IconButton from '@/components/common/IconButton.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { useToast } from '@/composables/useToast'
import {
  announcementExcerpt,
  announcementTypeDotColor,
  announcementTypeTone,
  formatUnixDateTime,
} from '@/features/announcements/lib/announcementHelpers'
import { useAnnouncementStore } from '@/stores/announcements'
import { useAuthStore } from '@/stores/auth'

import AnnouncementDetailModal from './AnnouncementDetailModal.vue'

const { t, locale } = useI18n()
const router = useRouter()
const toast = useToast()
const auth = useAuthStore()
const store = useAnnouncementStore()
const { items, unreadCount, loading, error } = storeToRefs(store)

const listOpen = ref(false)
const filterMode = ref<'all' | 'unread'>('all')
const selectedId = ref<number | null>(null)
const selectedFallback = ref<UserAnnouncement | null>(null)
const selected = computed<UserAnnouncement | null>(() => {
  if (selectedId.value === null) return null
  return (
    items.value.find((item) => item.id === selectedId.value) ??
    selectedFallback.value
  )
})
const markingAll = ref(false)

const canManage = computed(
  () => auth.isAdmin && auth.hasPermission('announcement', 'read')
)

const filterOptions = computed(() => [
  { value: 'all', label: t('announcements.allTab') },
  { value: 'unread', label: t('announcements.unreadTab') },
])

const visibleItems = computed(() =>
  filterMode.value === 'unread'
    ? items.value.filter((item) => item.read_at === null)
    : items.value
)

const triggerAriaLabel = computed(() =>
  unreadCount.value > 0
    ? t('announcements.triggerLabelUnread', { count: unreadCount.value })
    : t('announcements.triggerLabel')
)

const badgeText = computed(() =>
  unreadCount.value > 99 ? '99+' : String(unreadCount.value)
)

watch(
  () => auth.user?.id,
  () => {
    listOpen.value = false
    selectedId.value = null
    selectedFallback.value = null
  }
)

onMounted(() => store.start())
onBeforeUnmount(() => store.stop())

function openList() {
  listOpen.value = true
  void store.fetchAnnouncements(true)
}

function closeList() {
  listOpen.value = false
}

async function openDetail(item: UserAnnouncement) {
  selectedId.value = item.id
  selectedFallback.value = item
  if (item.read_at === null) {
    const ok = await store.markAsRead(item.id)
    if (!ok && selectedId.value === item.id) {
      toast.error(t('announcements.markReadFailed'))
    }
  }
}

function closeDetail() {
  selectedId.value = null
  selectedFallback.value = null
}

async function handleMarkRead(id: number) {
  const ok = await store.markAsRead(id)
  if (!ok) {
    toast.error(t('announcements.markReadFailed'))
    return
  }
  toast.success(t('announcements.markedRead'))
  closeDetail()
}

async function handleMarkAllRead() {
  if (markingAll.value || unreadCount.value === 0) return
  markingAll.value = true
  try {
    const ok = await store.markAllAsRead()
    if (!ok) {
      toast.error(t('announcements.markReadFailed'))
      return
    }
    toast.success(t('announcements.allMarkedRead'))
  } finally {
    markingAll.value = false
  }
}

function openManagement() {
  closeList()
  void router.push({ name: 'announcements' })
}
</script>

<template>
  <button
    type="button"
    data-announcement-trigger
    data-handdrawn="control"
    class="announcement-trigger relative inline-flex h-11 w-11 items-center justify-center rounded-full text-[var(--text-secondary)] transition-colors hover:bg-[var(--state-hover-layer)] hover:text-[var(--text-primary)] focus-ring"
    :class="unreadCount > 0 ? 'text-[var(--accent-text)]' : ''"
    :aria-label="triggerAriaLabel"
    :title="triggerAriaLabel"
    aria-haspopup="dialog"
    :aria-expanded="listOpen"
    @click="openList"
  >
    <Bell :size="18" :stroke-width="1.9" aria-hidden="true" />
    <span
      v-if="unreadCount > 0"
      data-announcement-badge
      aria-hidden="true"
      class="announcement-badge pointer-events-none absolute right-1.5 top-1.5 flex min-w-4 items-center justify-center rounded-full bg-[var(--status-danger)] px-1 text-[0.625rem] font-semibold leading-4 text-[var(--text-inverse)]"
    >
      {{ badgeText }}
    </span>
  </button>

  <ConsoleModal
    :open="listOpen"
    :aria-label="t('announcements.title')"
    size="lg"
    @close="closeList"
  >
    <template #header>
      <div
        class="flex min-w-0 flex-wrap items-center justify-between gap-3 text-left"
      >
        <div class="flex min-w-0 items-center gap-2.5">
          <span
            class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-[var(--accent-soft)] text-[var(--accent-text)]"
            aria-hidden="true"
          >
            <Bell :size="17" />
          </span>
          <div class="min-w-0">
            <h2
              class="display-title truncate text-lg font-bold text-[var(--text-primary)]"
            >
              {{ t('announcements.title') }}
            </h2>
            <p
              v-if="unreadCount > 0"
              class="text-xs font-medium text-[var(--accent-text)]"
            >
              {{ t('announcements.unreadCount', { count: unreadCount }) }}
            </p>
          </div>
        </div>

        <div class="flex items-center gap-1.5">
          <IconButton
            :label="t('announcements.refresh')"
            :disabled="loading"
            @click="store.fetchAnnouncements(true)"
          >
            <RefreshCw :size="15" :class="loading ? 'animate-spin' : ''" />
          </IconButton>
          <ConsoleButton
            v-if="unreadCount > 0"
            variant="secondary"
            size="sm"
            :loading="markingAll"
            data-announcement-mark-all
            @click="handleMarkAllRead"
          >
            <CheckCheck :size="14" aria-hidden="true" />
            {{ t('announcements.markAllRead') }}
          </ConsoleButton>
          <IconButton :label="t('common.close')" @click="closeList">
            <X :size="16" aria-hidden="true" />
          </IconButton>
        </div>
      </div>
    </template>

    <div class="space-y-3" data-announcement-list-modal>
      <div class="flex items-center justify-between gap-3">
        <SegmentedToggle
          v-model="filterMode"
          :options="filterOptions"
          :label="t('announcements.title')"
        />
      </div>

      <ErrorBanner v-if="error" :message="error">
        <template #action>
          <ConsoleButton
            variant="secondary"
            size="sm"
            @click="store.fetchAnnouncements(true)"
          >
            {{ t('announcements.retry') }}
          </ConsoleButton>
        </template>
      </ErrorBanner>

      <div v-if="loading && !items.length" class="space-y-2.5" aria-busy="true">
        <SkeletonBlock v-for="i in 3" :key="i" bordered class="h-20" />
      </div>

      <EmptyState
        v-else-if="!visibleItems.length"
        :title="
          filterMode === 'unread'
            ? t('announcements.emptyUnreadTitle')
            : t('announcements.emptyTitle')
        "
        :hint="
          filterMode === 'unread'
            ? t('announcements.emptyUnreadHint')
            : t('announcements.emptyHint')
        "
      />

      <ul v-else class="divide-y divide-[var(--border-subtle)]">
        <li v-for="item in visibleItems" :key="item.id">
          <button
            type="button"
            class="announcement-row group relative flex w-full items-start gap-3 rounded-xl px-3.5 py-3.5 text-left transition-colors hover:bg-[var(--state-hover-layer)] focus-ring"
            :class="item.read_at === null ? 'announcement-row--unread' : ''"
            :data-announcement-item="item.id"
            @click="openDetail(item)"
          >
            <span
              class="mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full"
              :style="{ background: announcementTypeDotColor(item.type) }"
              aria-hidden="true"
            />
            <div class="min-w-0 flex-1 space-y-1">
              <div class="flex flex-wrap items-center gap-1.5">
                <span
                  class="min-w-0 truncate text-sm font-semibold text-[var(--text-primary)]"
                >
                  {{ item.title }}
                </span>
                <StatusChip v-if="item.pinned" tone="accent">
                  <Pin :size="11" aria-hidden="true" />
                  {{ t('announcements.pinned') }}
                </StatusChip>
                <StatusChip :tone="announcementTypeTone(item.type)">
                  {{ t(`announcements.types.${item.type}`) }}
                </StatusChip>
                <StatusChip v-if="item.read_at === null" tone="accent">
                  {{ t('announcements.unread') }}
                </StatusChip>
              </div>
              <p class="line-clamp-2 text-xs text-[var(--text-secondary)]">
                {{ announcementExcerpt(item.content) }}
              </p>
              <time class="block text-[0.6875rem] text-[var(--text-tertiary)]">
                {{ formatUnixDateTime(item.created_at, locale) }}
              </time>
            </div>
            <ChevronRight
              :size="16"
              aria-hidden="true"
              class="mt-1 shrink-0 text-[var(--text-tertiary)] transition-transform group-hover:translate-x-0.5 group-hover:text-[var(--text-primary)]"
            />
          </button>
        </li>
      </ul>
    </div>

    <template v-if="canManage" #footer>
      <div class="flex items-center justify-between gap-3">
        <button
          type="button"
          data-announcement-manage-link
          class="inline-flex min-h-11 items-center gap-1.5 text-xs font-medium text-[var(--accent-text)] transition-opacity hover:underline focus-ring sm:min-h-0"
          @click="openManagement"
        >
          <span>{{ t('announcements.manageLink') }}</span>
          <ArrowRight :size="14" aria-hidden="true" />
        </button>
        <ConsoleButton variant="secondary" size="sm" @click="closeList">
          {{ t('common.close') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>

  <AnnouncementDetailModal
    :open="selected !== null"
    :announcement="selected"
    :z-index="95"
    @close="closeDetail"
    @mark-read="handleMarkRead"
  />
</template>

<style scoped>
.announcement-badge {
  box-shadow: 0 0 0 2px var(--surface-solid);
}

.announcement-row--unread {
  background: var(--accent-soft);
}

.announcement-row--unread::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0.625rem;
  bottom: 0.625rem;
  width: 2.5px;
  border-radius: 9999px;
  background: var(--accent);
}
</style>
