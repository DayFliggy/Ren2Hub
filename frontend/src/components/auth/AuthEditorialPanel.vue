<script setup lang="ts">
// 登录页左栏 — 编辑级排版叙事（随想式）：
// 中转动画（提示词 → 网关 → 多上游聚合转发）、衬线诗意大标题、
// 信任要点清单、点阵地球（AI 中转的世界意象）、真实运行数据脚注。
// 三个内容块与动画共用 --panel-measure，右边缘对齐成一条编辑级竖线。
// 全部颜色走语义令牌，与站点整页纸底/炭蓝双主题同源。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import AuthRelayFlow from '@/components/auth/AuthRelayFlow.vue'
import type { DotGlobe } from '@/canvas/DotGlobe'
import { useTheme } from '@/composables/useTheme'
import { useAppStore } from '@/stores'

const { t } = useI18n()
const { resolvedTheme } = useTheme()
const app = useAppStore()

const globeEl = ref<HTMLCanvasElement | null>(null)
const ready = ref(false)

const reducedMotion =
  typeof window !== 'undefined' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches

// Release builds report the full commit hash; show the conventional short
// form and keep the full value in the tooltip. Tagged versions stay intact.
const COMMIT_HASH = /^[0-9a-f]{10,40}$/i

const stats = computed(() => {
  const items: { label: string; value: string; title?: string }[] = [
    { label: t('auth.tickerModels'), value: app.modelCountLabel },
  ]
  // Without an enabled uptime source that reported data there is nothing to show.
  if (app.uptimePercent !== null)
    items.push({ label: t('auth.tickerUptime'), value: app.uptimeLabel })
  const version = app.versionLabel
  items.push(
    COMMIT_HASH.test(version)
      ? {
          label: t('auth.tickerVersion'),
          value: version.slice(0, 9),
          title: version,
        }
      : { label: t('auth.tickerVersion'), value: version }
  )
  return items
})
// Only advertise open registration once the status confirms it.
const registrationOpen = computed(
  () => app.statusReachable && app.registerEnabled
)

let globe: DotGlobe | null = null
let disposed = false
let resizeObserver: ResizeObserver | null = null

function syncRunning() {
  if (!globe) return
  if (document.hidden) globe.stop()
  else globe.start()
}

onMounted(async () => {
  if (!globeEl.value) return
  if (globeEl.value.getBoundingClientRect().width === 0) return // hidden < lg
  try {
    const { DotGlobe } = await import('@/canvas/DotGlobe')
    if (disposed || !globeEl.value) return
    globe = new DotGlobe(globeEl.value, reducedMotion)
    globe.resize()
  } catch {
    globe?.dispose()
    globe = null
    return
  }
  if (disposed || !globe) return
  ready.value = true
  globe.start()
  resizeObserver = new ResizeObserver(() => globe?.resize())
  resizeObserver.observe(globeEl.value)
  document.addEventListener('visibilitychange', syncRunning)
})

watch(resolvedTheme, () => globe?.refreshPalette())

onBeforeUnmount(() => {
  disposed = true
  document.removeEventListener('visibilitychange', syncRunning)
  resizeObserver?.disconnect()
  globe?.dispose()
  globe = null
})
</script>

<template>
  <div
    class="auth-panel relative hidden flex-col overflow-hidden px-10 py-12 lg:flex xl:px-16"
  >
    <!-- 点阵地球随叙事区收放，沉入右下角，避开左侧信任清单。 -->
    <canvas
      ref="globeEl"
      class="pointer-events-none absolute -bottom-[16%] -right-[8%] h-[58%] w-[72%] opacity-0 transition-opacity duration-1000"
      :class="{ 'opacity-55': ready }"
      aria-hidden="true"
    />

    <!-- 顶部：中转动画 — 提示词 → 网关 → 多上游聚合转发 -->
    <div class="panel-measure relative z-10">
      <AuthRelayFlow />
    </div>

    <!-- 中部：标语 + 诗意大标题 + 信任要点 -->
    <div class="panel-measure relative z-10">
      <p
        class="panel-kicker flex items-center gap-3 text-xs tracking-[0.3em] text-[var(--text-tertiary)]"
      >
        <span>{{ t('auth.editorial.word1') }}</span>
        <span class="text-[var(--accent)]">·</span>
        <span>{{ t('auth.editorial.word2') }}</span>
        <span class="text-[var(--accent)]">·</span>
        <span>{{ t('auth.editorial.word3') }}</span>
      </p>

      <h2
        class="auth-huiwen-title display-title mt-6 leading-[1.14] text-[var(--text-primary)]"
      >
        <span class="block text-5xl font-bold xl:text-6xl">{{
          t('auth.editorial.line1')
        }}</span>
        <span
          class="mt-2 block text-5xl font-bold text-[var(--text-tertiary)] xl:text-6xl"
          >{{ t('auth.editorial.line2') }}</span
        >
      </h2>

      <div class="mt-10 h-px w-full bg-[var(--border-default)]" />

      <ul
        class="panel-promises mt-8 sibling-space-y-4 text-sm text-[var(--text-secondary)]"
      >
        <li class="flex items-center gap-3.5">
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
            stroke-linejoin="round"
            class="shrink-0 text-[var(--text-tertiary)]"
          >
            <rect x="4" y="10" width="16" height="11" rx="2" />
            <path d="M8 10V7a4 4 0 0 1 8 0v3" />
          </svg>
          {{ t('auth.editorial.point1') }}
        </li>
        <li class="flex items-center gap-3.5">
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
            stroke-linejoin="round"
            class="shrink-0 text-[var(--text-tertiary)]"
          >
            <path d="M12 2 4 6v6c0 5 3.4 8.6 8 10 4.6-1.4 8-5 8-10V6l-8-4Z" />
          </svg>
          {{ t('auth.editorial.point2') }}
        </li>
        <li
          v-if="registrationOpen"
          class="flex items-center gap-3.5"
          data-registration-claim
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
            stroke-linejoin="round"
            class="shrink-0 text-[var(--text-tertiary)]"
          >
            <circle cx="12" cy="12" r="9" />
            <path d="M12 7v5l3.5 2" />
          </svg>
          {{ t('auth.editorial.point3') }}
        </li>
      </ul>
    </div>

    <!-- 底部：运行数据与上方叙事分区，超高屏下仍留在有界的内容区内。 -->
    <div class="panel-measure relative z-10">
      <div class="h-px w-full bg-[var(--border-default)]" />
      <p
        class="panel-status mt-5 flex items-center gap-2.5 text-[11px] font-semibold uppercase tracking-[0.22em] text-[var(--text-tertiary)]"
      >
        <span
          class="inline-block h-1.5 w-1.5 rounded-full bg-[var(--accent)]"
        />
        {{ t('auth.editorial.statusLabel') }}
      </p>
      <div class="panel-stats mt-3 flex flex-wrap gap-x-8 gap-y-1.5">
        <p
          v-for="s in stats"
          :key="s.label"
          class="text-sm text-[var(--text-tertiary)]"
        >
          {{ s.label }}
          <span
            class="ml-1.5 font-mono font-semibold text-[var(--text-primary)]"
            :title="s.title"
            >{{ s.value }}</span
          >
        </p>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 恢复流程、主叙事、状态三段层次。高度有上限，避免高屏把脚注推至屏幕底部；
   只有最小高度，矮屏和长文案可自然撑开，不裁切内容。 */
.auth-panel {
  align-self: center;
  min-height: min(calc(100svh - var(--auth-header-height)), 70rem);
  justify-content: space-between;
  gap: clamp(1.5rem, 3.5vh, 4rem);
}

.auth-huiwen-title,
.auth-huiwen-title span {
  font-family: 'Ren2AuthHuiwen', var(--font-display);
  font-weight: 400;
  letter-spacing: 0;
  font-synthesis: none;
}

/* 单一测量线：动画、标题块、脚注共用同一宽度，右边缘对齐。
   1024 档留出余量给中转动画的上游簇；1280 起放宽到编辑级行长。 */
.panel-measure {
  width: 100%;
  max-width: 34rem;
}

@media (min-width: 1280px) {
  .panel-measure {
    max-width: 38rem;
  }
}

@media (min-width: 1441px) {
  .panel-measure {
    max-width: 64rem;
  }

  .auth-huiwen-title span {
    font-size: clamp(3.75rem, min(3.4vw, 8vh), 6rem);
    text-wrap: balance;
  }

  .panel-kicker,
  .panel-status {
    font-size: var(--auth-caption-size);
  }

  .panel-promises,
  .panel-stats p {
    font-size: var(--auth-body-size);
    line-height: 1.6;
  }

  .panel-promises > li + li {
    margin-top: var(--auth-field-gap);
  }

  .panel-promises svg {
    width: 1.15em;
    height: 1.15em;
  }
}
</style>
