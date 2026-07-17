<template>
  <div
    class="min-h-screen bg-[var(--page-background)] text-[var(--text-primary)]"
  >
    <AppNavbar />
    <ScrollActivityIndicator />

    <!-- ===== 开屏 Hero ===== -->
    <section
      class="relative flex min-h-[100svh] items-center overflow-hidden pt-[calc(env(safe-area-inset-top)+5.5rem)] pb-[calc(env(safe-area-inset-bottom)+4.75rem)] lg:min-h-screen lg:pt-20 lg:pb-0"
    >
      <!-- 全屏背景：点阵世界地图 + 网关路由动画 -->
      <HeroWorldMap @signal-change="signalSnapshot = $event" />

      <!-- 深度渐变叠层：聚光网关枢纽(左中)、压暗四周 -->
      <div
        class="pointer-events-none absolute inset-0"
        aria-hidden="true"
        style="background: var(--hero-depth-overlay)"
      />
      <!-- 右侧文字可读性 scrim（桌面端压暗右栏底衬） -->
      <div
        class="pointer-events-none absolute inset-0 hidden lg:block"
        aria-hidden="true"
        style="background: var(--hero-copy-overlay)"
      />
      <!-- 移动端整体轻压暗，保证居中文字可读 -->
      <div
        class="pointer-events-none absolute inset-0 lg:hidden"
        aria-hidden="true"
        style="background: var(--hero-mobile-overlay)"
      />
      <!-- 暗角 -->
      <div
        class="pointer-events-none absolute inset-0"
        aria-hidden="true"
        style="box-shadow: var(--hero-vignette)"
      />

      <!-- 左栏：与 Canvas 联动的 ASCII 网关信号账本 -->
      <HeroSignalConsole :snapshot="signalSnapshot" />

      <!-- 两栏内容（左栏由 Signal Console + Canvas 场景承接；右栏文本） -->
      <div
        class="relative z-10 mx-auto grid w-full max-w-[100rem] grid-cols-1 items-center gap-6 px-4 py-6 sm:px-6 sm:py-8 lg:grid-cols-[minmax(18rem,0.78fr)_minmax(34rem,1.22fr)] lg:gap-10 lg:px-8 lg:py-14 xl:gap-14 xl:py-16"
      >
        <!-- 左：留空（地图与网关枢纽由全屏背景层透出） -->
        <div class="order-1 hidden lg:order-none lg:block" aria-hidden="true" />
        <!-- 右：文本与组件 -->
        <div
          class="order-2 flex w-full justify-center lg:order-none lg:w-auto lg:justify-end"
        >
          <HeroContent />
        </div>
      </div>

      <!-- 向下滚动提示 -->
      <div
        class="pointer-events-none absolute bottom-5 left-1/2 hidden -translate-x-1/2 lg:block"
        aria-hidden="true"
      >
        <div
          class="flex h-9 w-5 items-start justify-center rounded-full border border-[var(--border-default)] p-1.5"
        >
          <span
            class="h-2 w-1 animate-bounce rounded-full bg-[var(--accent)]"
          />
        </div>
      </div>
    </section>

    <AppFooter />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import AppNavbar from '@/components/layout/AppNavbar.vue'
import AppFooter from '@/components/layout/AppFooter.vue'
import HeroWorldMap from '@/components/hero/HeroWorldMap.vue'
import HeroContent from '@/components/hero/HeroContent.vue'
import HeroSignalConsole from '@/components/hero/HeroSignalConsole.vue'
import ScrollActivityIndicator from '@/components/hero/ScrollActivityIndicator.vue'
import { useHeroScrollChrome } from '@/composables/useHeroScrollChrome'
import {
  INITIAL_SIGNAL_SNAPSHOT,
  type SignalConsoleSnapshot,
} from '@/data/signalConsole'

const signalSnapshot = ref<SignalConsoleSnapshot>({
  ...INITIAL_SIGNAL_SNAPSHOT,
})

useHeroScrollChrome()
</script>
