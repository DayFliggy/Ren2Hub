<template>
  <div
    class="flex w-full max-w-xl flex-col items-center text-center lg:max-w-[38rem] lg:items-end lg:text-right xl:max-w-[42rem]"
  >
    <!-- 眉标 -->
    <p
      class="animate-fade-in font-mono text-[11px] font-bold uppercase tracking-[0.18em] text-[var(--accent)] sm:text-xs sm:tracking-[0.2em]"
      style="animation-delay: 0.15s"
    >
      {{ t('hero.eyebrow') }}
    </p>

    <!-- 主标题 + 打字机 -->
    <h1
      class="mt-3 text-[clamp(2rem,10vw,2.5rem)] font-extrabold leading-[1.08] tracking-tight text-[var(--text-primary)] sm:mt-4 sm:text-5xl sm:leading-[1.15] lg:text-[3.4rem]"
    >
      <span class="animate-rise-in" style="animation-delay: 0.25s">{{
        t('hero.titleLead')
      }}</span>
      <br />
      <span class="gradient-text">{{ displayed }}</span
      ><span class="type-cursor" />
    </h1>

    <!-- 副标题 -->
    <p
      class="mt-4 max-w-lg animate-fade-in text-[15px] leading-relaxed text-[var(--text-secondary)] sm:mt-5 sm:text-lg"
      style="animation-delay: 0.5s"
    >
      {{ t('hero.subtitle') }}
    </p>

    <!-- 承诺胶囊 -->
    <ul
      class="mt-4 flex flex-wrap justify-center gap-1.5 animate-fade-in sm:mt-5 sm:gap-2 lg:justify-end"
      style="animation-delay: 0.65s"
    >
      <li
        v-for="p in pledges"
        :key="p"
        class="inline-flex items-center gap-1.5 rounded-full border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-2.5 py-1 text-[11px] text-[var(--text-secondary)] sm:px-3 sm:text-xs"
      >
        <svg
          class="text-[var(--accent)]"
          width="13"
          height="13"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.6"
          aria-hidden="true"
        >
          <path d="M20 6L9 17l-5-5" />
        </svg>
        {{ p }}
      </li>
    </ul>

    <!-- CTA -->
    <div
      class="mt-6 flex w-full flex-col items-stretch gap-2 animate-fade-in sm:mt-7 lg:mt-8 lg:w-auto lg:flex-row lg:items-center lg:gap-3 lg:justify-end"
      style="animation-delay: 0.8s"
    >
      <a
        :href="primaryHref"
        class="group relative inline-flex w-full items-center justify-center gap-2 overflow-hidden rounded-full bg-[var(--accent)] px-7 py-3.5 text-base font-semibold text-[var(--accent-contrast)] transition-all hover:-translate-y-0.5 hover:bg-[var(--accent-hover)] hover:shadow-[0_16px_36px_var(--shadow-color)] lg:w-auto"
      >
        <span class="cta-sheen" />
        <span class="relative">{{
          isAuthenticated ? t('nav.console') : t('hero.ctaPrimary')
        }}</span>
        <svg
          class="relative transition-transform group-hover:translate-x-0.5"
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
          aria-hidden="true"
        >
          <path d="M5 12h14M13 6l6 6-6 6" />
        </svg>
      </a>
      <a
        v-if="showDocs"
        :href="docsLink"
        target="_blank"
        rel="noopener noreferrer"
        class="group inline-flex min-h-11 items-center justify-center gap-2 self-center px-3 py-2 text-sm font-medium text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)] lg:min-h-0 lg:self-auto lg:rounded-full lg:border lg:border-[var(--border-default)] lg:px-7 lg:py-3.5 lg:text-base lg:font-semibold lg:text-[var(--text-secondary)] lg:transition-all lg:hover:border-[var(--border-strong)] lg:hover:bg-[var(--surface-muted)]"
      >
        {{ t('hero.ctaSecondary') }}
        <svg
          class="transition-transform group-hover:translate-x-0.5"
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
          aria-hidden="true"
        >
          <path d="M9 6l6 6-6 6" />
        </svg>
      </a>
    </div>

    <!-- 指标 -->
    <dl
      class="mt-8 grid w-full grid-cols-3 gap-2 text-center animate-fade-in sm:mt-9 sm:gap-4 lg:flex lg:w-auto lg:gap-8 lg:text-right lg:justify-end"
      style="animation-delay: 0.95s"
    >
      <div v-for="m in metrics" :key="m.label" class="min-w-0">
        <dt
          class="font-mono text-xl font-bold text-[var(--text-primary)] sm:text-2xl"
        >
          {{ m.value }}
        </dt>
        <dd
          class="mt-0.5 break-words text-[10px] leading-tight text-[var(--text-tertiary)] sm:text-xs sm:leading-normal"
        >
          {{ m.label }}
        </dd>
      </div>
    </dl>

    <!-- 已适配工具跑马灯 -->
    <div
      class="mt-7 w-full min-w-0 overflow-visible border-t border-[var(--border-subtle)] pt-4 animate-fade-in sm:mt-8 lg:mt-11 xl:mt-12"
      style="animation-delay: 1.1s"
    >
      <IntegrationMarquee />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { useTyped } from '@/composables/useTyped'
import IntegrationMarquee from './IntegrationMarquee.vue'
import { useAppStore } from '@/stores'

const { t, tm } = useI18n()
const {
  docsLink,
  showDocs,
  primaryHref,
  isAuthenticated,
  modelCountLabel,
  uptimeLabel,
  versionLabel,
} = storeToRefs(useAppStore())

const typedWords = computed(() => tm('hero.typed') as string[])
const { displayed } = useTyped(typedWords)

const pledges = computed(() => tm('hero.pledges') as string[])

const metrics = computed(() => [
  { value: modelCountLabel.value, label: t('hero.metrics.models') },
  { value: uptimeLabel.value, label: t('hero.metrics.uptime') },
  { value: versionLabel.value, label: t('hero.metrics.version') },
])
</script>
