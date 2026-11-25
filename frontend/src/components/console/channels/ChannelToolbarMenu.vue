<script setup lang="ts">
import { onClickOutside } from '@vueuse/core'
import {
  Activity,
  ArrowDownNarrowWide,
  ArrowUpNarrowWide,
  Check,
  Coins,
  Layers,
  LoaderCircle,
  MoreHorizontal,
  Wallet,
} from 'lucide-vue-next'
import { computed, nextTick, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'

import IconButton from '@/components/common/IconButton.vue'
import {
  tabLeavesPanel,
  useFloatingPanel,
} from '@/composables/useFloatingPanel'
import { ADMIN_CHANNEL_SORT_FIELDS } from '@/constants/adminChannels'
import type { AdminChannelSortBy, AdminChannelSortOrder } from '@/types/console'

const props = defineProps<{
  groupByLab: boolean
  sortBy: AdminChannelSortBy
  sortOrder: AdminChannelSortOrder
  canOperate: boolean
  balanceAllBusy: boolean
  balanceAllDisabled?: boolean
  pageBalanceBusy: boolean
  pageBalanceProgress?: string
  pageBalanceDisabled: boolean
  pageBalanceCount: number
  batchTestDisabled: boolean
}>()

const emit = defineEmits<{
  'update:groupByLab': [value: boolean]
  'update:sortBy': [value: AdminChannelSortBy]
  'update:sortOrder': [value: AdminChannelSortOrder]
  startBalanceAll: []
  runPageBalance: []
  openPageTest: []
}>()

const { t } = useI18n()

const root = ref<HTMLElement | null>(null)
const trigger = ref<InstanceType<typeof IconButton> | null>(null)
const panel = ref<HTMLElement | null>(null)
const open = ref(false)
const panelId = useId()
const titleId = useId()

const isBusy = computed(() => props.balanceAllBusy || props.pageBalanceBusy)
const hasActiveViewOptions = computed(
  () => props.groupByLab || props.sortBy !== 'id' || props.sortOrder !== 'desc'
)

async function openMenu() {
  open.value = true
  await nextTick()
  panel.value
    ?.querySelector<HTMLButtonElement>('button:not([disabled])')
    ?.focus()
}

function closeMenu({ restoreFocus = false } = {}) {
  open.value = false
  if (restoreFocus) nextTick(() => trigger.value?.focus())
}

function toggleMenu() {
  if (open.value) closeMenu({ restoreFocus: true })
  else void openMenu()
}

function onKeydown(event: KeyboardEvent) {
  if (!open.value) return
  const leaving = panel.value && tabLeavesPanel(event, panel.value)
  if (event.key !== 'Escape' && !leaving) return
  event.preventDefault()
  event.stopPropagation()
  closeMenu({ restoreFocus: true })
}

function onFocusout() {
  window.requestAnimationFrame(() => {
    const active = document.activeElement
    if (root.value?.contains(active) || panel.value?.contains(active)) return
    closeMenu()
  })
}

function triggerAndClose(action: () => void) {
  closeMenu({ restoreFocus: true })
  action()
}

onClickOutside(root, () => closeMenu(), { ignore: [panel] })
const { style: panelStyle, scope: panelScope } = useFloatingPanel(
  root,
  panel,
  open,
  { align: 'end' }
)
</script>

<template>
  <div ref="root" class="relative" @keydown="onKeydown" @focusout="onFocusout">
    <IconButton
      ref="trigger"
      :label="t('channels.moreActions')"
      class="pencil-control relative rounded-xl border border-[var(--border-default)] bg-[var(--surface-solid)]"
      data-handdrawn="control"
      data-channel-toolbar-menu-trigger
      aria-haspopup="dialog"
      :aria-expanded="open"
      :aria-controls="panelId"
      @click="toggleMenu"
    >
      <LoaderCircle
        v-if="isBusy"
        :size="16"
        class="animate-spin text-[var(--accent-text)]"
      />
      <MoreHorizontal v-else :size="16" />
      <span
        v-if="hasActiveViewOptions && !isBusy"
        class="pointer-events-none absolute right-1.5 top-1.5 h-1.5 w-1.5 rounded-full bg-[var(--accent)]"
        aria-hidden="true"
      />
    </IconButton>

    <Teleport to="body">
      <Transition name="toolbar-menu">
        <div
          v-if="open"
          :id="panelId"
          ref="panel"
          role="dialog"
          aria-modal="false"
          :aria-labelledby="titleId"
          class="subtle-scroll max-h-[min(28rem,calc(100dvh-8rem))] w-72 overflow-y-auto rounded-lg border border-[var(--overlay-border)] bg-[var(--surface-overlay)] py-1.5"
          :style="panelStyle"
          data-handdrawn="menu"
          data-floating-panel
          data-channel-toolbar-menu
          :data-handdrawn-scope="panelScope"
          @keydown="onKeydown"
          @focusout="onFocusout"
        >
          <span :id="titleId" class="sr-only">{{
            t('channels.moreActions')
          }}</span>

          <!-- View section -->
          <div class="px-3 py-1.5">
            <p
              class="text-[0.6875rem] font-semibold uppercase tracking-wider text-[var(--text-tertiary)]"
            >
              {{ t('channels.menuViewSection') }}
            </p>
          </div>
          <button
            type="button"
            role="switch"
            :aria-checked="groupByLab"
            data-channel-group-toggle
            class="flex w-full items-center justify-between gap-2.5 px-3 py-2 text-left text-sm text-[var(--text-primary)] transition-colors hover:bg-[var(--state-hover-layer)] focus-ring"
            @click="emit('update:groupByLab', !groupByLab)"
          >
            <span class="flex min-w-0 items-center gap-2.5">
              <Layers
                :size="15"
                class="shrink-0 text-[var(--text-tertiary)]"
                aria-hidden="true"
              />
              <span class="truncate">{{ t('channels.groupByLab') }}</span>
            </span>
            <span
              class="flex h-4 w-4 shrink-0 items-center justify-center rounded-sm border transition-colors"
              :class="
                groupByLab
                  ? 'border-[var(--accent)] bg-[var(--accent)] text-[var(--accent-contrast)]'
                  : 'border-[var(--border-default)]'
              "
              aria-hidden="true"
            >
              <Check v-if="groupByLab" :size="12" :stroke-width="3" />
            </span>
          </button>

          <!-- Sort section -->
          <div
            class="my-1 border-t border-[var(--border-subtle)]"
            role="separator"
          />
          <div class="flex items-center justify-between gap-2 px-3 py-1.5">
            <p
              class="text-[0.6875rem] font-semibold uppercase tracking-wider text-[var(--text-tertiary)]"
            >
              {{ t('channels.menuSortSection') }}
            </p>
            <div
              class="inline-flex items-center gap-0.5 rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-container-low)] p-0.5"
              role="group"
              :aria-label="t('channels.menuSortSection')"
            >
              <button
                type="button"
                data-channel-sort-order="asc"
                class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors focus-ring"
                :class="
                  sortOrder === 'asc'
                    ? 'bg-[var(--surface-solid)] text-[var(--text-primary)] shadow-xs'
                    : 'text-[var(--text-tertiary)] hover:text-[var(--text-primary)]'
                "
                :aria-pressed="sortOrder === 'asc'"
                :aria-label="t('channels.sortAscending')"
                @click="emit('update:sortOrder', 'asc')"
              >
                <ArrowUpNarrowWide :size="12" aria-hidden="true" />
                {{ t('channels.sortOrderAscShort') }}
              </button>
              <button
                type="button"
                data-channel-sort-order="desc"
                class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors focus-ring"
                :class="
                  sortOrder === 'desc'
                    ? 'bg-[var(--surface-solid)] text-[var(--text-primary)] shadow-xs'
                    : 'text-[var(--text-tertiary)] hover:text-[var(--text-primary)]'
                "
                :aria-pressed="sortOrder === 'desc'"
                :aria-label="t('channels.sortDescending')"
                @click="emit('update:sortOrder', 'desc')"
              >
                <ArrowDownNarrowWide :size="12" aria-hidden="true" />
                {{ t('channels.sortOrderDescShort') }}
              </button>
            </div>
          </div>
          <div class="px-1.5 pb-1">
            <button
              v-for="field in ADMIN_CHANNEL_SORT_FIELDS"
              :key="field"
              type="button"
              :aria-pressed="sortBy === field"
              :data-channel-sort-option="field"
              class="flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-[var(--state-hover-layer)] focus-ring"
              :class="
                sortBy === field
                  ? 'font-semibold text-[var(--accent-text)]'
                  : 'text-[var(--text-primary)]'
              "
              @click="emit('update:sortBy', field)"
            >
              <span>{{ t(`channels.sort.${field}`) }}</span>
              <Check
                v-if="sortBy === field"
                :size="14"
                :stroke-width="2.5"
                aria-hidden="true"
              />
            </button>
          </div>

          <!-- Batch tools section -->
          <template v-if="canOperate">
            <div
              class="my-1 border-t border-[var(--border-subtle)]"
              role="separator"
            />
            <div class="px-3 py-1.5">
              <p
                class="text-[0.6875rem] font-semibold uppercase tracking-wider text-[var(--text-tertiary)]"
              >
                {{ t('channels.menuBatchSection') }}
              </p>
            </div>
            <button
              type="button"
              :disabled="balanceAllBusy || balanceAllDisabled"
              data-channel-menu-balance-all
              class="flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm text-[var(--text-primary)] transition-colors enabled:hover:bg-[var(--state-hover-layer)] focus-ring disabled:cursor-not-allowed disabled:opacity-40"
              @click="triggerAndClose(() => emit('startBalanceAll'))"
            >
              <LoaderCircle
                v-if="balanceAllBusy"
                :size="15"
                class="shrink-0 animate-spin text-[var(--accent-text)]"
                aria-hidden="true"
              />
              <Wallet
                v-else
                :size="15"
                class="shrink-0 text-[var(--text-tertiary)]"
                aria-hidden="true"
              />
              <span class="min-w-0 flex-1 truncate">{{
                t('pluginExtensions.balanceAll')
              }}</span>
            </button>
            <button
              type="button"
              :disabled="pageBalanceDisabled"
              data-channel-menu-balance-page
              class="flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm text-[var(--text-primary)] transition-colors enabled:hover:bg-[var(--state-hover-layer)] focus-ring disabled:cursor-not-allowed disabled:opacity-40"
              @click="triggerAndClose(() => emit('runPageBalance'))"
            >
              <LoaderCircle
                v-if="pageBalanceBusy"
                :size="15"
                class="shrink-0 animate-spin text-[var(--accent-text)]"
                aria-hidden="true"
              />
              <Coins
                v-else
                :size="15"
                class="shrink-0 text-[var(--text-tertiary)]"
                aria-hidden="true"
              />
              <span class="min-w-0 flex-1 truncate">{{
                t('channels.batchBalancePage')
              }}</span>
              <span
                class="shrink-0 font-mono text-xs tabular-nums text-[var(--text-tertiary)]"
              >
                {{
                  pageBalanceBusy && pageBalanceProgress
                    ? pageBalanceProgress
                    : pageBalanceCount
                }}
              </span>
            </button>
            <button
              type="button"
              :disabled="batchTestDisabled"
              data-channel-menu-batch-test
              class="flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm text-[var(--text-primary)] transition-colors enabled:hover:bg-[var(--state-hover-layer)] focus-ring disabled:cursor-not-allowed disabled:opacity-40"
              @click="triggerAndClose(() => emit('openPageTest'))"
            >
              <Activity
                :size="15"
                class="shrink-0 text-[var(--text-tertiary)]"
                aria-hidden="true"
              />
              <span class="min-w-0 flex-1 truncate">{{
                t('channels.batchTestPage')
              }}</span>
            </button>
          </template>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.toolbar-menu-enter-active,
.toolbar-menu-leave-active {
  transition:
    opacity 0.12s ease,
    transform 0.12s ease;
}

.toolbar-menu-enter-from,
.toolbar-menu-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
