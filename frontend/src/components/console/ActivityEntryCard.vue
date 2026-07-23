<script setup lang="ts">
import { ref } from 'vue'

defineProps<{
  title: string
  subtitle: string
  tag: string
  image: string
  /** CSS gradient shown before image loads or on image error */
  gradient: string
  emoji: string
  cta: string
}>()

const emit = defineEmits<{ enter: [] }>()

const imageOk = ref(true)
</script>

<template>
  <button
    type="button"
    class="group relative flex h-44 w-full overflow-hidden rounded-2xl border border-[var(--border-subtle)] text-left shadow-[var(--card-shadow)] transition-all hover:shadow-[var(--card-shadow-hover)] focus-ring"
    @click="emit('enter')"
  >
    <!-- background gradient (always present as base layer) -->
    <div class="absolute inset-0" :style="{ background: gradient }" />

    <!-- generated banner art -->
    <img
      v-if="imageOk"
      :src="image"
      :alt="title"
      class="absolute inset-0 h-full w-full object-cover transition-transform duration-500 group-hover:scale-105"
      @error="imageOk = false"
    />

    <!-- readability scrim -->
    <div
      class="absolute inset-0"
      style="
        background: linear-gradient(
          90deg,
          rgba(0, 0, 0, 0.62) 0%,
          rgba(0, 0, 0, 0.32) 55%,
          rgba(0, 0, 0, 0.05) 100%
        );
      "
    />

    <!-- content -->
    <div class="relative z-10 flex flex-col justify-between p-5">
      <div>
        <span
          class="inline-flex items-center gap-1 rounded-full bg-white/20 px-2.5 py-1 text-xs font-semibold text-white backdrop-blur-sm"
        >
          {{ emoji }} {{ tag }}
        </span>
        <h3
          class="mt-2.5 text-xl font-bold tracking-tight text-white drop-shadow"
        >
          {{ title }}
        </h3>
        <p class="mt-1 max-w-xs text-sm leading-relaxed text-white/80">
          {{ subtitle }}
        </p>
      </div>

      <span
        class="mt-4 inline-flex items-center gap-1.5 text-sm font-semibold text-white"
      >
        {{ cta }}
        <svg
          width="15"
          height="15"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
          class="transition-transform group-hover:translate-x-1"
        >
          <path d="M5 12h14M13 6l6 6-6 6" />
        </svg>
      </span>
    </div>
  </button>
</template>
