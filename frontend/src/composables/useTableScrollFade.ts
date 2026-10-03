import { onScopeDispose, ref, watch, type Ref } from 'vue'

/**
 * Tracks whether a horizontally scrollable container still hides content past
 * its right edge. The container is followed through its ref rather than bound
 * once at mount: tables render behind loading branches and are replaced when
 * their data reloads, so the element that exists at mount is rarely the one
 * the user scrolls.
 */
export function useTableScrollFade(containerRef: Ref<HTMLElement | null>) {
  const canScrollRight = ref(false)

  function updateScrollFade() {
    const el = containerRef.value
    if (!el) {
      canScrollRight.value = false
      return
    }
    const hasOverflow = el.scrollWidth > el.clientWidth + 1
    const atEnd = el.scrollLeft + el.clientWidth >= el.scrollWidth - 1
    canScrollRight.value = hasOverflow && !atEnd
  }

  const observer = new ResizeObserver(updateScrollFade)
  let bound: HTMLElement | null = null

  function unbind() {
    if (!bound) return
    bound.removeEventListener('scroll', updateScrollFade)
    observer.unobserve(bound)
    bound = null
  }

  // flush 'post': the ref only points at the new element after the DOM patch.
  watch(
    containerRef,
    (el) => {
      if (el !== bound) {
        unbind()
        if (el) {
          el.addEventListener('scroll', updateScrollFade, { passive: true })
          observer.observe(el)
          bound = el
        }
      }
      updateScrollFade()
    },
    { flush: 'post', immediate: true }
  )

  onScopeDispose(() => {
    unbind()
    observer.disconnect()
  })

  return {
    canScrollRight,
    updateScrollFade,
  }
}
