<script setup lang="ts">
// 编辑式认证布局（随想式）：整页同一纸底（texture-paper），顶部一条极简
// 品牌栏，左侧编辑排版叙事（AuthEditorialPanel），右侧悬浮表单卡。
// 双主题依旧全令牌驱动 —— 日间暖纸、夜间炭蓝，无恒深面板。
// 站名从 useAppStore.systemName 读取，与控制台侧边栏/Topbar 保持一致。
import { useI18n } from 'vue-i18n'

import AuthEditorialPanel from '@/components/auth/AuthEditorialPanel.vue'
import BrandMark from '@/components/console/BrandMark.vue'
import LanguageSelector from '@/components/common/LanguageSelector.vue'
import ThemeSwitcher from '@/components/common/ThemeSwitcher.vue'
import { useAppStore } from '@/stores'

const { t } = useI18n()
const app = useAppStore()
</script>

<template>
  <div
    class="auth-layout night-page-texture texture-paper draft-grid flex min-h-screen flex-col bg-[var(--page-background)]"
    data-handdrawn-scope="auth"
  >
    <!-- 品牌栏与主体共用容器，标准桌面限宽，大屏随内容尺寸一起展开。 -->
    <header
      class="auth-header auth-container relative z-20 mx-auto flex h-16 w-full max-w-[1440px] shrink-0 items-center justify-between px-5 sm:px-8 xl:px-12"
    >
      <RouterLink
        :to="{ name: 'home' }"
        class="flex items-center gap-2.5 focus-ring rounded-lg"
        :aria-label="app.systemName"
      >
        <BrandMark class="auth-brand-mark h-8 w-8 rounded-lg" />
        <span
          class="auth-brand-name display-title text-lg font-bold tracking-tight text-[var(--text-primary)]"
          >{{ app.systemName }}</span
        >
      </RouterLink>
      <div class="flex items-center gap-1 sm:gap-2">
        <ThemeSwitcher variant="console" />
        <LanguageSelector variant="console" />
        <RouterLink
          :to="{ name: 'home' }"
          class="ml-1 hidden items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm text-[var(--text-tertiary)] transition-colors hover:text-[var(--text-primary)] focus-ring sm:flex"
        >
          ← {{ t('auth.backHome') }}
        </RouterLink>
      </div>
    </header>

    <!-- 主体：左侧分区叙事，右侧表单卡垂直居中。 -->
    <div
      class="auth-body auth-container mx-auto grid w-full max-w-[1440px] flex-1 lg:grid-cols-[1fr_minmax(420px,480px)] lg:gap-10 lg:px-8 xl:gap-16 xl:px-12"
    >
      <AuthEditorialPanel />

      <!-- 表单卡：纸面上的悬浮实体卡 -->
      <div class="flex items-center justify-center px-4 py-8 lg:py-12">
        <div
          class="auth-card pencil-surface-strong w-full max-w-md border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-7 py-9 sm:px-9"
          data-handdrawn="surface-strong"
        >
          <slot />
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.auth-layout {
  --auth-header-height: 4rem;
}

.auth-header {
  height: var(--auth-header-height);
}

/* Sketch radius + elevation come from the theme fork: hand-drawn card by day,
   Material card by night. */
.auth-card {
  border-radius: var(--sketch-border-radius-lg);
  box-shadow: var(--elevation-3);
}

/* Grow both columns beyond the standard desktop layout. Text and vertical
   spacing also depend on height so an ultrawide, short display stays usable. */
@media (min-width: 1441px) {
  .auth-layout {
    --auth-header-height: clamp(4rem, min(3.3vw, 7vh), 6rem);
    --auth-body-size: clamp(0.875rem, min(0.66vw, 1.6vh), 1.125rem);
    --auth-caption-size: clamp(0.75rem, min(0.55vw, 1.4vh), 0.9375rem);
    --auth-control-size: clamp(3rem, min(2.1vw, 5vh), 3.75rem);
    --auth-field-gap: clamp(1rem, min(0.85vw, 2.1vh), 1.75rem);
  }

  .auth-container {
    max-width: clamp(90rem, calc(70vw + 27rem), 160rem);
    padding-inline: clamp(3rem, calc(1.43vw + 1.71rem), 5rem);
  }

  .auth-brand-mark {
    width: clamp(2rem, 1.4vw, 3rem);
    height: clamp(2rem, 1.4vw, 3rem);
  }

  .auth-brand-name {
    font-size: clamp(1.125rem, 0.9vw, 1.625rem);
  }

  .auth-body {
    grid-template-columns: minmax(0, 1fr) clamp(
        30rem,
        calc(17.5vw + 14.25rem),
        46rem
      );
    column-gap: clamp(4rem, calc(3.57vw + 0.79rem), 7rem);
  }

  .auth-card {
    max-width: none;
    padding: clamp(2.25rem, min(2.1vw, 4.5vh), 4rem);
  }

  .auth-card :deep(h1) {
    font-size: clamp(1.875rem, min(1.75vw, 4vh), 3.25rem);
    line-height: 1.25;
  }

  .auth-card :deep(form) {
    margin-top: clamp(2rem, min(1.65vw, 4vh), 3rem);
  }

  .auth-card :deep(form > :not([hidden]) ~ :not([hidden])) {
    margin-top: var(--auth-field-gap);
  }

  .auth-card :deep(label > span),
  .auth-card :deep(p.text-sm) {
    font-size: var(--auth-body-size);
  }

  .auth-card :deep(p.text-xs),
  .auth-card :deep(a.text-xs) {
    font-size: var(--auth-caption-size);
  }

  .auth-card :deep(input:not([type='checkbox'])),
  .auth-card :deep(button[data-handdrawn='control']) {
    min-height: var(--auth-control-size);
    font-size: clamp(0.9375rem, min(0.7vw, 1.7vh), 1.125rem);
  }
}
</style>
