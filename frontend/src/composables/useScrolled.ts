import { onMounted, onUnmounted, ref } from 'vue'

/**
 * 监听窗口滚动，返回是否越过阈值（用于导航栏毛玻璃切换）。
 * 同一帧内的多次 scroll 合并为一次读取，卸载时取消未执行的帧。
 */
export function useScrolled(threshold = 20) {
  const scrolled = ref(false)
  let frame: number | undefined

  const update = () => {
    frame = undefined
    scrolled.value = window.scrollY > threshold
  }

  const onScroll = () => {
    if (frame === undefined) frame = window.requestAnimationFrame(update)
  }

  onMounted(() => {
    update()
    window.addEventListener('scroll', onScroll, { passive: true })
  })

  onUnmounted(() => {
    window.removeEventListener('scroll', onScroll)
    if (frame !== undefined) window.cancelAnimationFrame(frame)
    frame = undefined
  })

  return { scrolled }
}
