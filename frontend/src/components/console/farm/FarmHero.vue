<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import type { FarmState, MineState } from '@/types/farm'
import farmBanner from '@/assets/activity/farm-banner.webp'

const props = defineProps<{
  state: FarmState
  mine: MineState
}>()

const { t } = useI18n()

const expPercent = computed(() =>
  props.state.exp_next > 0
    ? Math.round((props.state.exp / props.state.exp_next) * 100)
    : 100
)
</script>

<template>
  <section
    class="relative overflow-hidden rounded-2xl border border-[var(--border-subtle)] p-5 shadow-[var(--card-shadow)]"
  >
    <!-- generated banner art -->
    <img
      :src="farmBanner"
      alt=""
      aria-hidden="true"
      class="absolute inset-0 h-full w-full object-cover"
    />
    <!-- readability scrim: darkens for white text, keeps art visible on the right -->
    <div
      class="absolute inset-0"
      style="
        background: linear-gradient(
          100deg,
          rgba(20, 24, 14, 0.9) 0%,
          rgba(20, 24, 14, 0.66) 48%,
          rgba(20, 24, 14, 0.28) 100%
        );
      "
    />
    <div class="relative z-10">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <!-- left: level + exp -->
        <div class="flex items-center gap-4">
          <!-- level badge -->
          <div
            class="flex size-14 shrink-0 flex-col items-center justify-center rounded-2xl text-center"
            style="background: var(--accent); color: var(--accent-contrast)"
          >
            <span
              class="text-[10px] font-semibold uppercase tracking-wider opacity-80"
              >FARM</span
            >
            <span class="text-xl font-bold leading-none">{{
              state.level
            }}</span>
          </div>
          <div>
            <p class="text-xs text-white/70">
              {{ t('farm.level', { n: state.level }) }}
            </p>
            <p class="text-lg font-bold text-white drop-shadow">
              {{ t('farm.exp', { cur: state.exp, max: state.exp_next }) }}
            </p>
            <!-- exp bar -->
            <div
              class="mt-1.5 h-1.5 w-40 overflow-hidden rounded-full bg-white/25"
            >
              <div
                class="h-full rounded-full transition-all duration-500"
                style="background: var(--accent)"
                :style="{ width: `${expPercent}%` }"
              />
            </div>
          </div>
        </div>

        <!-- right: resource chips -->
        <div class="flex flex-wrap gap-2">
          <div
            class="flex items-center gap-1.5 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3 py-1.5 text-sm"
          >
            <span>🪙</span>
            <span class="font-semibold text-[var(--accent-text)]">{{
              state.coins.toLocaleString()
            }}</span>
            <span class="text-[var(--text-tertiary)]">{{
              t('farm.coins')
            }}</span>
          </div>
          <div
            class="flex items-center gap-1.5 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3 py-1.5 text-sm"
          >
            <span>⛏️</span>
            <span class="font-semibold text-[var(--text-primary)]">{{
              state.ore
            }}</span>
            <span class="text-[var(--text-tertiary)]">{{ t('farm.ore') }}</span>
          </div>
          <div
            class="flex items-center gap-1.5 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3 py-1.5 text-sm"
          >
            <span>🌾</span>
            <span class="font-semibold text-[var(--text-primary)]">{{
              state.feed
            }}</span>
            <span class="text-[var(--text-tertiary)]">{{
              t('farm.feed')
            }}</span>
          </div>
        </div>
      </div>

      <!-- mine stats bar -->
      <div
        v-if="mine"
        class="mt-4 flex flex-wrap items-center gap-x-4 gap-y-1 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-4 py-2.5 text-xs"
      >
        <span class="flex items-center gap-1 text-[var(--text-secondary)]">
          ⛏️ {{ t('farm.mine.title') }}
        </span>
        <span class="text-[var(--text-tertiary)]">{{
          t('farm.mine.todayCalls', { n: mine.today_calls })
        }}</span>
        <span class="text-[var(--text-tertiary)]">{{
          t('farm.mine.todayOre', { n: mine.today_ore })
        }}</span>
        <span
          v-if="mine.bonus_active"
          class="rounded-md px-2 py-0.5 font-semibold"
          style="
            background: var(--status-warning-soft);
            color: var(--status-warning-text);
          "
        >
          ⚡ {{ t('farm.mine.bonusActive') }}
        </span>
        <span class="ml-auto text-[var(--text-tertiary)]">{{
          t('farm.mine.hint')
        }}</span>
      </div>
    </div>
  </section>
</template>
