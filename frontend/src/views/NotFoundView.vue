<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<template>
  <main
    class="grid min-h-screen place-items-center bg-[var(--page-background)] px-6 text-center text-[var(--text-primary)]"
  >
    <div>
      <p class="font-mono text-sm font-semibold text-[var(--accent)]">404</p>
      <h1 class="mt-3 text-3xl font-bold">{{ t('notFound.title') }}</h1>
      <a
        href="/"
        class="mt-6 inline-flex rounded-full bg-[var(--accent)] px-5 py-2.5 font-semibold text-[var(--accent-contrast)]"
      >
        {{ t('notFound.backHome') }}
      </a>
    </div>
  </main>
</template>
