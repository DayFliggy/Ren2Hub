<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import type { GameWallet } from '@/types/bigame'
import bigameBanner from '@/assets/activity/bigame-banner.webp'

defineProps<{ wallet: GameWallet }>()
const { t } = useI18n()
</script>

<template>
  <section
    class="relative overflow-hidden rounded-2xl border border-[var(--border-subtle)] p-5 shadow-[var(--card-shadow)]"
  >
    <!-- generated banner art -->
    <img
      :src="bigameBanner"
      alt=""
      aria-hidden="true"
      class="absolute inset-0 h-full w-full object-cover"
    />
    <!-- readability scrim -->
    <div
      class="absolute inset-0"
      style="
        background: linear-gradient(
          100deg,
          rgba(24, 18, 40, 0.9) 0%,
          rgba(24, 18, 40, 0.66) 48%,
          rgba(24, 18, 40, 0.28) 100%
        );
      "
    />
    <div class="relative z-10 flex flex-wrap items-start justify-between gap-4">
      <!-- title block -->
      <div>
        <span
          class="inline-flex items-center rounded-full px-3 py-1 text-xs font-semibold"
          style="background: var(--accent-soft); color: var(--accent-text)"
        >
          🎮 {{ t('bigame.title') }}
        </span>
        <h1
          class="mt-3 text-2xl font-bold tracking-tight text-white drop-shadow"
        >
          {{ t('bigame.earnTitle') }}
        </h1>
        <p class="mt-1 text-sm text-white/80">{{ t('bigame.earnHint') }}</p>
      </div>

      <!-- coin wallet stat card -->
      <div class="flex gap-3">
        <div
          class="rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-5 py-3 text-center"
        >
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ t('bigame.coinBalance') }}
          </p>
          <p class="mt-1 text-2xl font-bold" style="color: var(--accent-text)">
            🎰 {{ wallet.balance }}
          </p>
        </div>
        <div
          class="hidden rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-5 py-3 text-center sm:block"
        >
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ t('bigame.totalEarned') }}
          </p>
          <p class="mt-1 text-xl font-bold text-[var(--text-primary)]">
            {{ wallet.total_earned }}
          </p>
        </div>
        <div
          class="hidden rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-5 py-3 text-center sm:block"
        >
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ t('bigame.totalSpent') }}
          </p>
          <p class="mt-1 text-xl font-bold text-[var(--text-primary)]">
            {{ wallet.total_spent }}
          </p>
        </div>
      </div>
    </div>
  </section>
</template>
