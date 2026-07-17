<template>
  <canvas
    ref="canvasEl"
    class="absolute inset-0 h-full w-full cursor-pointer"
    :aria-label="t('canvas.alt')"
    role="img"
  />
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { MapScene } from '@/canvas/MapScene'
import type { SignalConsoleSnapshot } from '@/data/signalConsole'
import { useTheme } from '@/composables/useTheme'

const emit = defineEmits<{
  signalChange: [snapshot: SignalConsoleSnapshot]
}>()

const { t } = useI18n()
const { resolvedTheme } = useTheme()
const canvasEl = ref<HTMLCanvasElement | null>(null)
let scene: MapScene | null = null
let io: IntersectionObserver | null = null

watch(resolvedTheme, (theme) => scene?.setTheme(theme))

const onResize = useDebounceFn(() => scene?.resize(), 180)

// 事件挂到 window 而非 canvas：右侧 HeroContent 等 HTML 层盖在画布上会吞掉指针事件，
// 导致中间/右侧模型无法悬停聚焦。改为按 canvas 矩形自行换算坐标，让悬停「穿透」到画布，
// 命中身后的模型；点击若落在真实可交互元素(a/button)上则放行，不劫持。
function localXY(
  e: MouseEvent
): { x: number; y: number; inside: boolean; insideY: boolean } | null {
  const el = canvasEl.value
  if (!el) return null
  const r = el.getBoundingClientRect()
  const x = e.clientX - r.left
  const y = e.clientY - r.top
  const insideY = y >= 0 && y <= r.height
  const inside = x >= 0 && x <= r.width && insideY
  return { x, y, inside, insideY }
}

function isCanvasInteractionBoundary(target: EventTarget | null) {
  if (!(target instanceof Element)) return false
  return Boolean(
    target.closest(
      '[data-hero-canvas-boundary], [data-canvas-interaction-boundary]'
    )
  )
}

function onWindowMove(e: MouseEvent) {
  if (isCanvasInteractionBoundary(e.target)) {
    resetInteraction()
    return
  }

  const p = localXY(e)
  if (!p) return
  const edgeActive =
    p.insideY && e.clientX >= 0 && e.clientX <= window.innerWidth
  scene?.setEdgePointer(e.clientX, window.innerWidth, edgeActive)
  if (p.inside) scene?.setMouse(p.x, p.y, true)
  else scene?.setMouse(0, 0, false) // 移出画布区域视作离开
}

function resetInteraction() {
  scene?.setMouse(0, 0, false)
  scene?.setEdgePointer(0, window.innerWidth, false)
}

function onWindowClick(e: MouseEvent) {
  if (isCanvasInteractionBoundary(e.target)) {
    resetInteraction()
    return
  }

  const p = localXY(e)
  if (!p || !p.inside) return
  // 落在真实可交互元素上(CTA/链接/按钮/跑马灯)：放行原生行为，不发起画布请求
  if (
    (e.target as HTMLElement)?.closest(
      'a, button, [role="button"], input, textarea, select, label'
    )
  )
    return
  scene?.clickAt(p.x, p.y)
}

function onWindowFocusIn(e: FocusEvent) {
  if (isCanvasInteractionBoundary(e.target)) {
    resetInteraction()
  }
}

// 运行闸门：仅当「在视口内」且「页面可见」时才跑动画。
// IntersectionObserver 管滚动离屏，visibilitychange/pagehide 管切后台标签页/隐藏——
// 二者共同决定，避免后台空转烧 CPU（借鉴 Midjourney 的 visibilitychange 暂停）。
let intersecting = true
function syncRunning() {
  if (!scene) return
  if (intersecting && !document.hidden) scene.start()
  else scene.stop()
}
function onVisibility() {
  if (document.hidden) resetInteraction()
  syncRunning()
}
function onPageHide() {
  resetInteraction()
  scene?.stop()
}

onMounted(async () => {
  if (!canvasEl.value) return
  scene = new MapScene(
    canvasEl.value,
    (snapshot) => emit('signalChange', snapshot),
    resolvedTheme.value
  )
  await scene.init()
  scene.start()
  window.addEventListener('resize', onResize)
  window.addEventListener('pointermove', onWindowMove, { passive: true })
  window.addEventListener('mousemove', onWindowMove, { passive: true })
  window.addEventListener('click', onWindowClick)
  window.addEventListener('focusin', onWindowFocusIn)
  window.addEventListener('blur', resetInteraction)
  document.addEventListener('visibilitychange', onVisibility)
  window.addEventListener('pagehide', onPageHide)

  io = new IntersectionObserver(
    ([entry]) => {
      intersecting = entry.isIntersecting
      if (!intersecting) resetInteraction()
      syncRunning()
    },
    { threshold: 0 }
  )
  io.observe(canvasEl.value)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  window.removeEventListener('pointermove', onWindowMove)
  window.removeEventListener('mousemove', onWindowMove)
  window.removeEventListener('click', onWindowClick)
  window.removeEventListener('focusin', onWindowFocusIn)
  window.removeEventListener('blur', resetInteraction)
  document.removeEventListener('visibilitychange', onVisibility)
  window.removeEventListener('pagehide', onPageHide)
  io?.disconnect()
  scene?.dispose()
  scene = null // 卸载后防抖尾调用触发时 scene?.resize() 即为 no-op
})
</script>
