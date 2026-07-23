<script setup lang="ts">
import activityBanner from '@/assets/activity/activity-banner.webp'

defineProps<{
  title: string
  subtitle: string
  pill: string
}>()
</script>

<template>
  <section
    class="relative overflow-hidden rounded-2xl shadow-[var(--card-shadow)]"
  >
    <!-- generated banner art -->
    <img
      :src="activityBanner"
      alt=""
      aria-hidden="true"
      class="absolute inset-0 h-full w-full object-cover"
    />
    <!-- readability scrim -->
    <div
      class="absolute inset-0"
      style="
        background: linear-gradient(
          105deg,
          rgba(16, 12, 8, 0.82) 0%,
          rgba(16, 12, 8, 0.52) 50%,
          rgba(16, 12, 8, 0.15) 100%
        );
      "
    />

    <!-- content -->
    <div class="relative z-10 px-8 py-8 sm:py-10">
      <span
        class="inline-flex items-center rounded-full bg-white/20 px-3 py-1 text-xs font-semibold text-white backdrop-blur-sm"
      >
        {{ pill }}
      </span>
      <h1
        class="mt-3 text-3xl font-bold tracking-tight text-white drop-shadow sm:text-4xl"
      >
        {{ title }}
      </h1>
      <p class="mt-2 max-w-xl text-sm leading-relaxed text-white/80">
        {{ subtitle }}
      </p>
    </div>
  </section>
</template>
