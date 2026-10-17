<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import ConfiguredContent from '@/components/public/ConfiguredContent.vue'

const { t } = useI18n()
const app = useAppStore()
</script>

<template>
  <footer
    class="border-t border-[var(--footer-border)] bg-[var(--surface-footer)] px-4 py-6 text-center text-xs leading-6 text-[var(--footer-text-tertiary)] sm:px-6"
  >
    <ConfiguredContent
      v-if="app.status.footer_html?.trim()"
      :content="app.status.footer_html"
      :title="t('footer.customContent')"
      variant="footer"
      class="mx-auto mb-4 h-28 max-w-4xl"
    />
    <p>
      {{ t('footer.copyright') }}
      <span class="mx-1 text-[var(--footer-border)]" aria-hidden="true">·</span>
      {{ t('footer.designBy') }}
      <strong class="font-semibold text-[var(--footer-text-primary)]">
        DayFliggy
      </strong>
      {{ t('footer.updated') }}
    </p>
    <p>
      <a
        href="https://github.com/QuantumNous/new-api"
        target="_blank"
        rel="noopener noreferrer"
        class="underline underline-offset-2"
        >Frontend design and development by New API contributors.</a
      >
    </p>
  </footer>
</template>
