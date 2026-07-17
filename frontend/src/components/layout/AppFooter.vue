<template>
  <footer
    class="relative border-t border-[var(--footer-border)] bg-[var(--surface-footer)]"
  >
    <div
      class="mx-auto max-w-7xl px-4 py-10 pb-[calc(env(safe-area-inset-bottom)+2.5rem)] sm:px-6 sm:py-12 sm:pb-[calc(env(safe-area-inset-bottom)+3rem)] lg:px-8 lg:pb-12"
    >
      <div class="flex flex-col gap-8 md:flex-row md:justify-between md:gap-10">
        <!-- 品牌 -->
        <div class="max-w-none sm:max-w-xs">
          <div class="flex items-center gap-2.5">
            <img
              :src="logo"
              :alt="systemName"
              class="h-8 w-8 rounded-lg object-contain"
            />
            <span
              class="max-w-64 truncate text-lg font-bold text-[var(--footer-text-primary)]"
              >{{ systemName }}</span
            >
          </div>
          <p
            class="mt-3 text-sm leading-relaxed text-[var(--footer-text-tertiary)]"
          >
            {{ t('footer.tagline') }}
          </p>
        </div>

        <!-- 链接组 -->
        <div
          class="grid w-full grid-cols-2 gap-x-5 gap-y-7 sm:gap-x-8 sm:gap-y-8 md:w-auto md:grid-cols-3"
        >
          <div>
            <h3
              class="font-mono text-[11px] font-bold uppercase tracking-wider text-[var(--footer-text-secondary)]"
            >
              {{ t('footer.product') }}
            </h3>
            <ul class="mt-2 space-y-0 text-sm md:mt-3 md:space-y-2">
              <li v-if="showPricing">
                <a
                  href="/pricing"
                  class="inline-flex min-h-11 items-center py-2 text-[var(--footer-text-tertiary)] transition-colors hover:text-[var(--footer-accent)] md:min-h-0 md:py-0"
                  >{{ t('footer.links.pricing') }}</a
                >
              </li>
              <li>
                <span
                  class="inline-flex min-h-11 items-center gap-2 py-2 text-[var(--footer-text-tertiary)] md:min-h-0 md:py-0"
                  ><StatusDot />{{
                    online ? t('nav.operational') : t('nav.degraded')
                  }}</span
                >
              </li>
            </ul>
          </div>
          <div>
            <h3
              class="font-mono text-[11px] font-bold uppercase tracking-wider text-[var(--footer-text-secondary)]"
            >
              {{ t('footer.resources') }}
            </h3>
            <ul class="mt-2 space-y-0 text-sm md:mt-3 md:space-y-2">
              <li v-if="showDocs">
                <a
                  :href="docsLink"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="inline-flex min-h-11 items-center py-2 text-[var(--footer-text-tertiary)] transition-colors hover:text-[var(--footer-accent)] md:min-h-0 md:py-0"
                  >{{ t('footer.links.docs') }}</a
                >
              </li>
              <li v-if="showAbout">
                <a
                  href="/about"
                  class="inline-flex min-h-11 items-center py-2 text-[var(--footer-text-tertiary)] transition-colors hover:text-[var(--footer-accent)] md:min-h-0 md:py-0"
                  >{{ t('footer.links.about') }}</a
                >
              </li>
            </ul>
          </div>
          <div class="col-span-2 md:col-span-1">
            <h3
              class="font-mono text-[11px] font-bold uppercase tracking-wider text-[var(--footer-text-secondary)]"
            >
              {{ t('footer.legal') }}
            </h3>
            <ul
              class="mt-2 flex flex-wrap gap-x-5 gap-y-0 text-sm md:mt-3 md:block md:space-y-2"
            >
              <li v-if="userAgreementEnabled">
                <a
                  href="/user-agreement"
                  class="inline-flex min-h-11 items-center py-2 text-[var(--footer-text-tertiary)] transition-colors hover:text-[var(--footer-accent)] md:min-h-0 md:py-0"
                  >{{ t('footer.links.terms') }}</a
                >
              </li>
              <li v-if="privacyPolicyEnabled">
                <a
                  href="/privacy-policy"
                  class="inline-flex min-h-11 items-center py-2 text-[var(--footer-text-tertiary)] transition-colors hover:text-[var(--footer-accent)] md:min-h-0 md:py-0"
                  >{{ t('footer.links.privacy') }}</a
                >
              </li>
            </ul>
          </div>
        </div>
      </div>

      <div
        class="mt-8 flex flex-col items-center justify-between gap-2 border-t border-[var(--footer-border)] pt-5 text-center text-xs text-[var(--footer-text-tertiary)] sm:mt-10 sm:flex-row sm:gap-3 sm:pt-6 sm:text-left"
      >
        <p>© {{ currentYear }} {{ systemName }}. {{ t('footer.rights') }}</p>
        <p v-if="versionLabel !== '--'" class="font-mono">
          v{{ versionLabel }}
        </p>
      </div>
    </div>
  </footer>
</template>

<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import StatusDot from '@/components/ui/StatusDot.vue'
import { useAppStore } from '@/stores'

const { t } = useI18n()
const currentYear = new Date().getFullYear()
const appStore = useAppStore()
const {
  systemName,
  logo,
  docsLink,
  showDocs,
  showPricing,
  showAbout,
  userAgreementEnabled,
  privacyPolicyEnabled,
  online,
  versionLabel,
} = storeToRefs(appStore)
</script>
