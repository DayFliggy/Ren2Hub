<template>
  <component
    :is="docsLink && !clone ? 'a' : 'span'"
    :aria-label="clone ? undefined : tool.name"
    :aria-hidden="clone ? 'true' : undefined"
    :role="!docsLink && !clone ? 'img' : undefined"
    :href="docsLink && !clone ? docsLink : undefined"
    :target="docsLink && !clone ? '_blank' : undefined"
    :rel="docsLink && !clone ? 'noopener noreferrer' : undefined"
    :tabindex="docsLink && !clone ? 0 : undefined"
    class="block shrink-0 overflow-visible"
  >
    <div
      class="hero-integration-chip group relative flex size-12 items-center justify-center rounded-full border transition-all duration-500 ease-[cubic-bezier(0.34,1.56,0.64,1)]"
      :class="
        active
          ? 'chip-active z-10 -translate-y-0.5 scale-110 border-[var(--accent)] bg-[var(--accent-soft)] opacity-100'
          : 'border-[var(--border-subtle)] bg-transparent opacity-75 hover:border-[var(--border-strong)] hover:opacity-100'
      "
      :title="tool.name"
    >
      <span
        v-if="active"
        class="hero-integration-chip__halo"
        aria-hidden="true"
      />
      <img
        v-if="!hasImgError"
        alt=""
        class="relative z-[1] size-7 object-contain transition-all duration-500"
        :class="active ? undefined : inactiveIconClass"
        :src="tool.icon"
        loading="lazy"
        @error="hasImgError = true"
      />
      <span
        v-else
        class="relative z-[1] flex size-7 items-center justify-center font-mono text-xs font-bold text-[var(--accent)] transition-all duration-500"
      >
        {{ tool.name.slice(0, 2).toUpperCase() }}
      </span>
    </div>
  </component>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { storeToRefs } from 'pinia'

import type { Integration } from '@/constants/home/integrations'
import { useAppStore } from '@/stores'

const { docsLink } = storeToRefs(useAppStore())
const hasImgError = ref(false)

const props = defineProps<{
  tool: Integration
  /** 是否处于聚光灯位（随滚动切换点亮） */
  active?: boolean
  /** 跑马灯的第二组克隆，屏蔽键盘/读屏 */
  clone?: boolean
}>()

// The chip's opacity is the one dimming layer. A grey single-tone mark vanished
// on a surface of its own tone, so marks dim to a neutral silhouette (dark grey
// by day, light grey by night) tuned to read like the greyed plates; plates
// carry their own background and only lose colour. Hover and the spotlight
// restore the original colours.
const inactiveIconClass = computed(() =>
  props.tool.tone === 'mark'
    ? 'brightness-0 invert-[0.3] dark:invert-[0.75] group-hover:filter-none'
    : 'grayscale group-hover:filter-none'
)
</script>

<style scoped>
.hero-integration-chip {
  isolation: isolate;
}
.hero-integration-chip__halo {
  position: absolute;
  z-index: 0;
  inset: -18px;
  border-radius: 999px;
  background: radial-gradient(
    circle,
    rgb(var(--accent-rgb) / 0.3) 0%,
    rgb(var(--accent-rgb) / 0.16) 38%,
    rgb(var(--accent-rgb) / 0) 72%
  );
  filter: blur(3px);
  pointer-events: none;
  animation: chip-halo 2.4s ease-in-out infinite;
}
/* 选中态：蜂蜜黄呼吸外发光（参考二狗 API 的满色高亮对比） */
.chip-active {
  animation: chip-breathe 2.4s ease-in-out infinite;
}
.chip-active::after {
  position: absolute;
  z-index: 2;
  inset: -6px;
  border: 1px solid rgb(var(--accent-rgb) / 0.45);
  border-radius: inherit;
  content: '';
  pointer-events: none;
  animation: chip-ring 2.4s ease-in-out infinite;
}
@keyframes chip-breathe {
  0%,
  100% {
    box-shadow:
      0 0 0 1px rgb(var(--accent-rgb) / 0.35),
      0 6px 18px -4px rgb(var(--accent-rgb) / 0.35);
  }
  50% {
    box-shadow:
      0 0 0 1px rgb(var(--accent-rgb) / 0.55),
      0 10px 26px -2px rgb(var(--accent-rgb) / 0.55);
  }
}
@keyframes chip-halo {
  0%,
  100% {
    opacity: 0.54;
    transform: scale(0.92);
  }
  50% {
    opacity: 1;
    transform: scale(1.08);
  }
}
@keyframes chip-ring {
  0%,
  100% {
    opacity: 0.42;
    transform: scale(0.96);
  }
  50% {
    opacity: 0.88;
    transform: scale(1.04);
  }
}
/* 移动端：光晕/描边环收敛（84px 光池 → 66px），不再溢出轨道渗入周边内容 */
@media (max-width: 1023px) {
  .hero-integration-chip__halo {
    inset: -9px;
    filter: blur(2px);
  }
  .chip-active::after {
    inset: -4px;
  }
}
@media (prefers-reduced-motion: reduce) {
  .hero-integration-chip__halo,
  .chip-active::after {
    animation: none;
  }
  .chip-active {
    animation: none;
    box-shadow:
      0 0 0 1px rgb(var(--accent-rgb) / 0.4),
      0 6px 18px -4px rgb(var(--accent-rgb) / 0.4);
  }
}
</style>
