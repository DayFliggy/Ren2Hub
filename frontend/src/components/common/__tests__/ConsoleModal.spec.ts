import { defineComponent, nextTick, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import ConsoleModal from '@/components/common/ConsoleModal.vue'

const StackedModals = defineComponent({
  components: { ConsoleModal },
  setup() {
    const outerOpen = ref(true)
    const innerOpen = ref(true)
    return { outerOpen, innerOpen }
  },
  template: `
    <ConsoleModal :open="outerOpen" title="Outer" @close="outerOpen = false">
      <button class="outer-action">Outer action</button>
    </ConsoleModal>
    <ConsoleModal :open="innerOpen" title="Inner" @close="innerOpen = false">
      <button class="inner-action">Inner action</button>
    </ConsoleModal>
  `,
})

describe('ConsoleModal', () => {
  it('has an accessible name, traps initial focus and restores the trigger', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()

    const wrapper = mount(ConsoleModal, {
      attachTo: document.body,
      props: { open: false, title: 'Delete token' },
      slots: { default: '<button class="confirm">Confirm</button>' },
    })

    await wrapper.setProps({ open: true })
    await nextTick()
    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.getAttribute('aria-labelledby')).toBeTruthy()
    expect(dialog?.classList.contains('pencil-surface-strong')).toBe(false)
    expect(
      dialog?.querySelector('[data-handdrawn="modal"]')?.classList
    ).toContain('pencil-surface-strong')
    expect(document.activeElement?.classList.contains('confirm')).toBe(true)

    await wrapper.setProps({ open: false })
    await nextTick()
    expect(document.activeElement).toBe(trigger)
    wrapper.unmount()
  })

  it('only lets the topmost dialog handle Escape', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()

    const wrapper = mount(StackedModals, { attachTo: document.body })
    await nextTick()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(wrapper.vm.innerOpen).toBe(false)
    expect(wrapper.vm.outerOpen).toBe(true)
    await new Promise((resolve) => window.setTimeout(resolve, 250))
    expect(document.body.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(document.body.style.overflow).toBe('hidden')

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(wrapper.vm.outerOpen).toBe(false)
    expect(document.body.style.overflow).toBe('')
    expect(document.activeElement).toBe(trigger)

    wrapper.unmount()
    trigger.remove()
  })

  it('hands keys to the dialog below while the closed one is still leaving', async () => {
    const wrapper = mount(StackedModals, { attachTo: document.body })
    try {
      await nextTick()

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      await nextTick()
      expect(wrapper.vm.innerOpen).toBe(false)
      // The inner dialog is still in its leave transition, on top of the outer.
      expect(document.body.querySelectorAll('[role="dialog"]')).toHaveLength(2)

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      await nextTick()
      expect(wrapper.vm.outerOpen).toBe(false)
    } finally {
      wrapper.unmount()
    }
  })

  it('handles keys again when reopened during its leave transition', async () => {
    const wrapper = mount(ConsoleModal, {
      attachTo: document.body,
      props: { open: true, title: 'Reopened' },
    })
    try {
      await nextTick()
      await wrapper.setProps({ open: false })
      await wrapper.setProps({ open: true })
      await nextTick()

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      expect(wrapper.emitted('close')).toHaveLength(1)
    } finally {
      wrapper.unmount()
    }
  })

  it('gives keys to the most recently opened dialog, not the last one in the DOM', async () => {
    // Mirrors the security proof (z-index 110) mounted before a form modal
    // whose close is disabled while it waits for that proof.
    const proof = mount(ConsoleModal, {
      attachTo: document.body,
      props: { open: false, title: 'Proof', zIndex: 110 },
      slots: {
        default:
          '<button class="proof-first">Verify</button><button class="proof-last">Cancel</button>',
      },
    })
    const form = mount(ConsoleModal, {
      attachTo: document.body,
      props: { open: false, title: 'Form', closeDisabled: true },
      slots: { default: '<button class="form-action">Save</button>' },
    })
    try {
      await form.setProps({ open: true })
      await proof.setProps({ open: true })
      await nextTick()

      const [proofDialog, formDialog] =
        document.body.querySelectorAll<HTMLElement>(
          '[role="dialog"][aria-modal="true"]'
        )
      expect(proofDialog?.textContent).toContain('Proof')
      expect(formDialog?.textContent).toContain('Form')
      const proofFirst =
        proofDialog!.querySelector<HTMLElement>('.proof-first')!
      const proofLast = proofDialog!.querySelector<HTMLElement>('.proof-last')!
      expect(document.activeElement).toBe(proofFirst)

      proofLast.focus()
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab' }))
      expect(document.activeElement).toBe(proofFirst)
      window.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true })
      )
      expect(document.activeElement).toBe(proofLast)

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      expect(proof.emitted('close')).toHaveLength(1)
      expect(form.emitted('close')).toBeUndefined()
    } finally {
      // Unmount even on failure: open modals hold the shared scroll lock.
      proof.unmount()
      form.unmount()
    }
  })

  it('ranks dialogs by open order rather than z-index', async () => {
    // The stack follows Sub2API's BaseDialog: whichever dialog opened last
    // handles Escape, whatever z-index an older dialog declares.
    const raised = mount(ConsoleModal, {
      attachTo: document.body,
      props: { open: false, title: 'Raised', zIndex: 110 },
    })
    const later = mount(ConsoleModal, {
      attachTo: document.body,
      props: { open: false, title: 'Later' },
    })
    try {
      await raised.setProps({ open: true })
      await later.setProps({ open: true })
      await nextTick()

      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      expect(later.emitted('close')).toHaveLength(1)
      expect(raised.emitted('close')).toBeUndefined()

      await later.setProps({ open: false })
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      expect(raised.emitted('close')).toHaveLength(1)
    } finally {
      raised.unmount()
      later.unmount()
    }
  })

  it('blocks Escape and backdrop dismissal while closing is disabled', async () => {
    const wrapper = mount(ConsoleModal, {
      attachTo: document.body,
      props: { open: true, title: 'Saving', closeDisabled: true },
    })
    await nextTick()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    document.body
      .querySelector<HTMLElement>('[role="dialog"] > .absolute.inset-0')
      ?.click()
    await nextTick()
    expect(wrapper.emitted('close')).toBeUndefined()

    await wrapper.setProps({ closeDisabled: false })
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('keeps the body locked when stacked modals close out of order', async () => {
    document.body.style.overflow = ''
    const first = mount(ConsoleModal, {
      attachTo: document.body,
      props: { open: false, title: 'first' },
    })
    const second = mount(ConsoleModal, {
      attachTo: document.body,
      props: { open: false, title: 'second' },
    })

    await first.setProps({ open: true })
    await second.setProps({ open: true })
    await nextTick()
    expect(document.body.style.overflow).toBe('hidden')

    // Closing the *older* modal first must not unlock while the newer one is
    // still open, and must not leave a stale "hidden" behind afterwards.
    await first.setProps({ open: false })
    await nextTick()
    expect(document.body.style.overflow).toBe('hidden')

    await second.setProps({ open: false })
    await nextTick()
    expect(document.body.style.overflow).toBe('')

    first.unmount()
    second.unmount()
  })

  it('releases its scroll lock when unmounted while open', async () => {
    document.body.style.overflow = ''
    const wrapper = mount(ConsoleModal, {
      attachTo: document.body,
      props: { open: false, title: 'unmount-while-open' },
    })

    await wrapper.setProps({ open: true })
    await nextTick()
    expect(document.body.style.overflow).toBe('hidden')

    wrapper.unmount()
    expect(document.body.style.overflow).toBe('')
  })

  it('supports the wide workbench size', async () => {
    const wrapper = mount(ConsoleModal, {
      attachTo: document.body,
      props: { open: true, title: 'Channel test', size: 'xl' },
    })
    await nextTick()

    expect(
      document.body.querySelector('[data-handdrawn="modal"]')?.classList
    ).toContain('max-w-4xl')

    wrapper.unmount()
  })
})
